package network

import (
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type ProxyDialerTestSuite struct {
	suite.Suite

	u *url.URL
}

func (suite *ProxyDialerTestSuite) SetupTest() {
	u, _ := url.Parse("socks5://hello:world@10.0.0.10:3128")
	suite.u = u
}

func (suite *ProxyDialerTestSuite) TestSetupDefaults() {
	d := newProxyDialer(&DialerMock{}, suite.u).(*cooldownDialer) //nolint: forcetypeassert
	suite.EqualValues(ProxyDialerOpenThreshold, d.openThreshold)
	suite.EqualValues(ProxyDialerReconnectTimeout, d.reconnectTimeout)
}

func (suite *ProxyDialerTestSuite) TestSetupValuesAllOk() {
	query := url.Values{}
	query.Set("open_threshold", "30")
	query.Set("reconnect_timeout", "2s")
	suite.u.RawQuery = query.Encode()

	d := newProxyDialer(&DialerMock{}, suite.u).(*cooldownDialer) //nolint: forcetypeassert
	suite.EqualValues(30, d.openThreshold)
	suite.EqualValues(2*time.Second, d.reconnectTimeout)
}

func (suite *ProxyDialerTestSuite) TestHalfOpenTimeoutBackwardCompat() {
	query := url.Values{}
	query.Set("half_open_timeout", "5s")
	suite.u.RawQuery = query.Encode()

	d := newProxyDialer(&DialerMock{}, suite.u).(*cooldownDialer) //nolint: forcetypeassert
	suite.EqualValues(5*time.Second, d.reconnectTimeout)
}

func (suite *ProxyDialerTestSuite) TestReconnectTimeoutOverridesHalfOpen() {
	query := url.Values{}
	query.Set("reconnect_timeout", "3s")
	query.Set("half_open_timeout", "10s")
	suite.u.RawQuery = query.Encode()

	d := newProxyDialer(&DialerMock{}, suite.u).(*cooldownDialer) //nolint: forcetypeassert
	suite.EqualValues(3*time.Second, d.reconnectTimeout)
}

func (suite *ProxyDialerTestSuite) TestOpenThreshold() {
	query := url.Values{}
	params := []string{"-30", "aaa", "1.0", "-1.0"}

	for _, v := range params {
		param := v
		suite.T().Run(v, func(t *testing.T) {
			query.Set("open_threshold", param)
			suite.u.RawQuery = query.Encode()

			d := newProxyDialer(&DialerMock{}, suite.u).(*cooldownDialer) //nolint: forcetypeassert
			assert.EqualValues(t, ProxyDialerOpenThreshold, d.openThreshold)
		})
	}
}

func (suite *ProxyDialerTestSuite) TestReconnectTimeout() {
	query := url.Values{}
	params := []string{"-30", "30", "aaa", "-3.0", "3.0"}

	for _, v := range params {
		param := v
		suite.T().Run(v, func(t *testing.T) {
			query.Set("reconnect_timeout", param)
			suite.u.RawQuery = query.Encode()

			d := newProxyDialer(&DialerMock{}, suite.u).(*cooldownDialer) //nolint: forcetypeassert
			assert.EqualValues(t, ProxyDialerReconnectTimeout, d.reconnectTimeout)
		})
	}
}

func TestProxyDialer(t *testing.T) {
	t.Parallel()
	suite.Run(t, &ProxyDialerTestSuite{})
}
