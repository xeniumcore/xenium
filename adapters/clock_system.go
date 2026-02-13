package adapters

import "time"

type SystemClock struct{}

func (SystemClock) UnixNano() int64 {
	return time.Now().UnixNano()
}
