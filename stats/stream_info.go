package stats

import (
	"time"

	statsd "github.com/smira/go-statsd"
)

type streamInfo struct {
	isDomainFronted bool
	tags            map[string]string
	startTime       time.Time // время начала сессии
	firstByteTime   time.Time // время получения первого байта (для TTFB)
	hasFirstByte    bool      // флаг получения первого байта
}

func (s streamInfo) T(key string) statsd.Tag {
	return statsd.StringTag(key, s.tags[key])
}

func (s *streamInfo) Reset() {
	s.isDomainFronted = false
	s.hasFirstByte = false
	s.startTime = time.Time{}
	s.firstByteTime = time.Time{}

	for k := range s.tags {
		delete(s.tags, k)
	}
}

func getDirection(isRead bool) string {
	if isRead { // for telegram
		return TagDirectionToClient
	}

	return TagDirectionFromClient
}
