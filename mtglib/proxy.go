package mtglib

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/9seconds/mtg/v2/essentials"
	"github.com/9seconds/mtg/v2/mtglib/internal/faketls"
	"github.com/9seconds/mtg/v2/mtglib/internal/faketls/record"
	"github.com/9seconds/mtg/v2/mtglib/internal/obfuscated2"
	"github.com/9seconds/mtg/v2/mtglib/internal/relay"
	"github.com/9seconds/mtg/v2/mtglib/internal/telegram"
	"github.com/panjf2000/ants/v2"
)

// isBrokenPipeError проверяет, является ли ошибка broken pipe или connection reset.
// Это происходит когда соединение из pool было закрыто Telegram до использования.
func isBrokenPipeError(err error) bool {
	if err == nil {
		return false
	}

	// Используем errors.Is для проверки syscall.Errno (Errno реализует Is() с Go 1.13)
	if errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET) {
		return true
	}

	// Fallback для wrapped ошибок где errors.Is не срабатывает
	errStr := err.Error()
	return strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "connection reset by peer")
}

// Proxy is an MTPROTO proxy structure.
type Proxy struct {
	ctx             context.Context
	ctxCancel       context.CancelFunc
	streamWaitGroup sync.WaitGroup

	allowFallbackOnUnknownDC bool
	fallbackOnDialError      bool
	tolerateTimeSkewness     time.Duration
	domainFrontingPort       int
	workerPool               *ants.PoolWithFunc
	telegram                 *telegram.Telegram
	config                   ProxyConfig
	rateLimiter              *RateLimiter

	secret          Secret
	network         Network
	antiReplayCache AntiReplayCache
	blocklist       IPBlocklist
	allowlist       IPBlocklist
	eventStream     EventStream
	logger          Logger
}

// DomainFrontingAddress returns a host:port pair for a fronting domain.
func (p *Proxy) DomainFrontingAddress() string {
	return net.JoinHostPort(p.secret.Host, strconv.Itoa(p.domainFrontingPort))
}

// ServeConn serves a connection. We do not check IP blocklist and concurrency
// limit here.
func (p *Proxy) ServeConn(conn essentials.Conn) {
	p.streamWaitGroup.Add(1)
	defer p.streamWaitGroup.Done()

	// Rate limiting check BEFORE creating stream context
	ipAddr := conn.RemoteAddr().(*net.TCPAddr).IP //nolint: forcetypeassert
	if p.rateLimiter != nil && !p.rateLimiter.Allow(ipAddr) {
		p.logger.BindStr("ip", hashIP(ipAddr)).Warning("Rate limited")
		p.eventStream.Send(p.ctx, NewEventConcurrencyLimited())
		conn.Close()

		return
	}

	ctx, err := newStreamContext(p.ctx, p.logger, conn)
	if err != nil {
		p.logger.WarningError("cannot create stream context", err)
		conn.Close()

		return
	}
	defer ctx.Close()

	// Handshake deadline: сбрасывается ЯВНО после хендшейка, а не через defer.
	// defer здесь нельзя — deadline остался бы активен во время relay, убивая
	// все соединения через HandshakeTimeout секунд.
	if p.config.HandshakeTimeout > 0 {
		conn.SetDeadline(time.Now().Add(p.config.HandshakeTimeout)) //nolint: errcheck
	}

	go func() {
		<-ctx.Done()
		ctx.Close()
	}()

	p.eventStream.Send(ctx, NewEventStart(ctx.streamID, ctx.ClientIP()))
	ctx.logger.Info("Stream has been started")

	defer func() {
		p.eventStream.Send(ctx, NewEventFinish(ctx.streamID))
		ctx.logger.Info("Stream has been finished")
	}()

	if !p.doFakeTLSHandshake(ctx) {
		return
	}

	if err := p.doObfuscated2Handshake(ctx); err != nil {
		p.logger.InfoError("obfuscated2 handshake is failed", err)

		return
	}

	// Хендшейк завершён — сбрасываем deadline перед relay.
	// TCP_USER_TIMEOUT (30s) в relay.go берёт на себя защиту от мёртвых соединений.
	conn.SetDeadline(time.Time{}) //nolint: errcheck

	if err := p.doTelegramCall(ctx); err != nil {
		// Не логировать спам для несуществующих DC (203, 999 и т.д.)
		if !strings.Contains(err.Error(), "invalid DC") {
			p.logger.WarningError("cannot dial to telegram", err)
		}

		return
	}

	relay.Relay(
		ctx,
		ctx.logger.Named("relay"),
		ctx.telegramConn,
		ctx.clientConn,
	)
}

// Serve starts a proxy on a given listener.
func (p *Proxy) Serve(listener net.Listener) error {
	p.streamWaitGroup.Add(1)
	defer p.streamWaitGroup.Done()

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-p.ctx.Done():
				return nil
			default:
				return fmt.Errorf("cannot accept a new connection: %w", err)
			}
		}

		ipAddr := conn.RemoteAddr().(*net.TCPAddr).IP //nolint: forcetypeassert
		logger := p.logger.BindStr("ip", hashIP(ipAddr))

		if !p.allowlist.Contains(ipAddr) {
			conn.Close()
			logger.Info("ip was rejected by allowlist")
			p.eventStream.Send(p.ctx, NewEventIPAllowlisted(ipAddr))

			continue
		}

		if p.blocklist.Contains(ipAddr) {
			conn.Close()
			logger.Info("ip was blacklisted")
			p.eventStream.Send(p.ctx, NewEventIPBlocklisted(ipAddr))

			continue
		}

		err = p.workerPool.Invoke(conn)

		switch {
		case err == nil:
		case errors.Is(err, ants.ErrPoolClosed):
			conn.Close()

			return nil
		case errors.Is(err, ants.ErrPoolOverload):
			conn.Close()
			logger.Info("connection was concurrency limited")
			p.eventStream.Send(p.ctx, NewEventConcurrencyLimited())
		}
	}
}

// Shutdown 'gracefully' shutdowns all connections. Please remember that it
// does not close an underlying listener.
func (p *Proxy) Shutdown() {
	p.ctxCancel()
	p.streamWaitGroup.Wait()
	p.workerPool.Release()

	p.allowlist.Shutdown()
	p.blocklist.Shutdown()

	// Остановка rate limiter cleanup goroutine (предотвращение goroutine leak)
	if p.rateLimiter != nil {
		p.rateLimiter.Stop()
	}

	// Закрытие connection pool к Telegram DC
	p.telegram.Close()
}

// GetPoolStats returns connection pool statistics for all DCs.
// Returns nil if connection pooling is disabled.
func (p *Proxy) GetPoolStats() []telegram.PoolStats {
	return p.telegram.PoolStats()
}

// GetRateLimiterSize returns number of tracked IPs in rate limiter.
// Returns 0 if rate limiting is disabled.
func (p *Proxy) GetRateLimiterSize() int {
	if p.rateLimiter == nil {
		return 0
	}

	return p.rateLimiter.Size()
}

func (p *Proxy) doFakeTLSHandshake(ctx *streamContext) bool {
	rec := record.AcquireRecord()
	defer record.ReleaseRecord(rec)

	rewind := newConnRewind(ctx.clientConn)

	if err := rec.Read(rewind); err != nil {
		p.logger.InfoError("cannot read client hello", err)
		p.doDomainFronting(ctx, rewind)

		return false
	}

	hello, err := faketls.ParseClientHello(p.secret.Key[:], rec.Payload.Bytes())
	if err != nil {
		p.logger.InfoError("cannot parse client hello", err)
		p.doDomainFronting(ctx, rewind)

		return false
	}

	if err := hello.Valid(p.secret.Host, p.tolerateTimeSkewness); err != nil {
		p.logger.
			BindStr("hostname", hello.Host).
			BindStr("hello-time", hello.Time.String()).
			InfoError("invalid faketls client hello", err)
		p.doDomainFronting(ctx, rewind)

		return false
	}

	if p.antiReplayCache.SeenBefore(hello.SessionID) {
		p.logger.Warning("replay attack has been detected!")
		p.eventStream.Send(p.ctx, NewEventReplayAttack(ctx.streamID))
		p.doDomainFronting(ctx, rewind)

		return false
	}

	if err := faketls.SendWelcomePacket(rewind, p.secret.Key[:], hello); err != nil {
		p.logger.InfoError("cannot send welcome packet", err)

		return false
	}

	ctx.clientConn = &faketls.Conn{
		Conn: ctx.clientConn,
	}

	return true
}

func (p *Proxy) doObfuscated2Handshake(ctx *streamContext) error {
	dc, encryptor, decryptor, err := obfuscated2.ClientHandshake(p.secret.Key[:], ctx.clientConn)
	if err != nil {
		return fmt.Errorf("cannot process client handshake: %w", err)
	}

	ctx.dc = dc
	ctx.logger = ctx.logger.BindInt("dc", dc)
	ctx.clientConn = obfuscated2.Conn{
		Conn:      ctx.clientConn,
		Encryptor: encryptor,
		Decryptor: decryptor,
	}

	return nil
}

func (p *Proxy) doTelegramCall(ctx *streamContext) error {
	dc := ctx.dc
	originalDC := dc

	// Telegram официально поддерживает только DC 1-5
	// Отклонять запросы к несуществующим DC (203, 999 и т.д.) без логирования
	if !p.telegram.IsKnownDC(dc) {
		if p.allowFallbackOnUnknownDC {
			dc = p.telegram.GetFallbackDC()
			ctx.logger = ctx.logger.BindInt("fallback_dc", dc)
			ctx.logger.Warning("unknown DC, fallbacks")
		} else {
			// Silent reject для DC > 5 - избегаем спама в логах
			return fmt.Errorf("invalid DC %d (only DC 1-5 are supported)", dc)
		}
	}

	conn, err := p.telegram.Dial(ctx, dc)
	if err != nil {
		// Fallback to another DC on dial error
		if p.fallbackOnDialError {
			fallbackDC := p.telegram.GetFallbackDCExcluding(dc)
			ctx.logger = ctx.logger.BindInt("original_dc", originalDC).BindInt("fallback_dc", fallbackDC)
			ctx.logger.Warning("DC unavailable, trying fallback")

			conn, err = p.telegram.Dial(ctx, fallbackDC)
			if err != nil {
				return fmt.Errorf("cannot dial to Telegram (fallback DC %d also failed): %w", fallbackDC, err)
			}

			dc = fallbackDC
		} else {
			return fmt.Errorf("cannot dial to Telegram: %w", err)
		}
	}

	encryptor, decryptor, err := obfuscated2.ServerHandshake(conn)
	if err != nil {
		// ForceClose: соединение с ошибкой handshake нельзя возвращать в пул
		if pc, ok := conn.(*telegram.PooledConn); ok {
			pc.ForceClose()
		} else {
			conn.Close()
		}

		// Retry с новым соединением при broken pipe (stale connection из pool)
		if isBrokenPipeError(err) {
			ctx.logger.Debug("broken pipe on handshake, retrying with fresh connection")

			// Получаем новое соединение напрямую (минуя pool)
			conn, err = p.telegram.DialDirect(ctx, dc)
			if err != nil {
				return fmt.Errorf("cannot dial to Telegram (retry): %w", err)
			}

			encryptor, decryptor, err = obfuscated2.ServerHandshake(conn)
			if err != nil {
				conn.Close()
				return fmt.Errorf("cannot perform obfuscated2 handshake (retry): %w", err)
			}
		} else {
			return fmt.Errorf("cannot perform obfuscated2 handshake: %w", err)
		}
	}

	// After obfuscated2 handshake, соединение имеет per-session протокольное состояние.
	// Unwrap PooledConn чтобы Close() реально закрыл TCP,
	// а не возвращал использованное соединение в пул.
	if pc, ok := conn.(*telegram.PooledConn); ok {
		conn = pc.Unwrap()
	}

	ctx.telegramConn = obfuscated2.Conn{
		Conn: newConnTraffic(conn, ctx.streamID, p.eventStream, ctx),
		Encryptor: encryptor,
		Decryptor: decryptor,
	}

	p.eventStream.Send(ctx,
		NewEventConnectedToDC(ctx.streamID,
			conn.RemoteAddr().(*net.TCPAddr).IP, //nolint: forcetypeassert
			dc),
	)

	return nil
}

func (p *Proxy) doDomainFronting(ctx *streamContext, conn *connRewind) {
	p.eventStream.Send(p.ctx, NewEventDomainFronting(ctx.streamID))
	conn.Rewind()

	frontConn, err := p.network.DialContext(ctx, "tcp", p.DomainFrontingAddress())
	if err != nil {
		p.logger.WarningError("cannot dial to the fronting domain", err)

		return
	}

	frontConn = newConnTraffic(frontConn, ctx.streamID, p.eventStream, ctx)

	relay.Relay(
		ctx,
		ctx.logger.Named("domain-fronting"),
		frontConn,
		conn,
	)
}

// NewProxy makes a new proxy instance.
func NewProxy(opts ProxyOpts) (*Proxy, error) {
	if err := opts.valid(); err != nil {
		return nil, fmt.Errorf("invalid settings: %w", err)
	}

	// Подготовка опций для telegram dialer
	var tgOpts []telegram.TelegramOption
	if opts.EnableConnectionPool {
		poolConfig := telegram.PoolConfig{
			MaxIdleConns:        opts.getConnectionPoolMaxIdle(),
			IdleTimeout:         opts.getConnectionPoolIdleTimeout(),
			HealthCheckInterval: 30 * time.Second,
		}
		tgOpts = append(tgOpts, telegram.WithConnectionPool(poolConfig))
	}

	// DC auto-refresh из JSON файла
	if opts.DCConfigFile != "" {
		tgOpts = append(tgOpts, telegram.WithDCConfigFile(
			opts.DCConfigFile,
			opts.DCRefreshInterval,
		))
	}

	tg, err := telegram.New(opts.Network, opts.getPreferIP(), opts.UseTestDCs, tgOpts...)
	if err != nil {
		return nil, fmt.Errorf("cannot build telegram dialer: %w", err)
	}

	// DNS pre-warming: resolve FakeTLS domain before accepting connections.
	// This reduces latency for the first client by 50-100ms.
	if opts.Secret.Host != "" {
		opts.Network.WarmUp([]string{opts.Secret.Host})
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Get config or use defaults
	config := opts.getConfig()

	// Create rate limiter if enabled
	var rateLimiter *RateLimiter
	if opts.getRateLimitPerSecond() > 0 {
		rateLimiter = NewRateLimiter(
			opts.getRateLimitPerSecond(),
			opts.getRateLimitBurst(),
			time.Minute, // cleanup every minute
		)
	}

	proxy := &Proxy{
		ctx:                      ctx,
		ctxCancel:                cancel,
		secret:                   opts.Secret,
		network:                  opts.Network,
		antiReplayCache:          opts.AntiReplayCache,
		blocklist:                opts.IPBlocklist,
		allowlist:                opts.IPAllowlist,
		eventStream:              opts.EventStream,
		logger:                   opts.getLogger("proxy"),
		domainFrontingPort:       opts.getDomainFrontingPort(),
		tolerateTimeSkewness:     opts.getTolerateTimeSkewness(),
		allowFallbackOnUnknownDC: opts.AllowFallbackOnUnknownDC,
		fallbackOnDialError:      opts.getFallbackOnDialError(),
		telegram:                 tg,
		config:                   config,
		rateLimiter:              rateLimiter,
	}

	pool, err := ants.NewPoolWithFunc(opts.getConcurrency(),
		func(arg interface{}) {
			proxy.ServeConn(arg.(essentials.Conn)) //nolint: forcetypeassert
		},
		ants.WithLogger(opts.getLogger("ants")),
		ants.WithNonblocking(true))
	if err != nil {
		return nil, fmt.Errorf("cannot create worker pool: %w", err)
	}

	proxy.workerPool = pool

	return proxy, nil
}
