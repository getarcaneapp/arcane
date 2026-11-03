package agent

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMuxSessionRecordRTT(t *testing.T) {
	session := &muxSession{}
	for _, rtt := range []time.Duration{30 * time.Millisecond, 10 * time.Millisecond, 50 * time.Millisecond} {
		session.recordRTT(rtt)
	}

	assert.Equal(t, int64(50*time.Millisecond), session.rtt.Load())
	assert.Equal(t, int64(10*time.Millisecond), session.rttMin.Load())
	assert.Equal(t, int64(50*time.Millisecond), session.rttMax.Load())
	assert.Equal(t, int64(90*time.Millisecond), session.rttSum.Load())
	assert.Equal(t, int64(3), session.rttCount.Load())
}

func TestMuxSessionTrafficBeforeStart(t *testing.T) {
	bytesIn, bytesOut := (&muxSession{}).traffic()
	assert.Zero(t, bytesIn)
	assert.Zero(t, bytesOut)
}
