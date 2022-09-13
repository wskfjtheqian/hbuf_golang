package utl

import (
	"context"
	"fmt"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"log"
	"time"
)

func ticker(ctx context.Context, t *time.Ticker, duration time.Duration, call func(ctx context.Context) error, init func(ctx context.Context) error) {
	if nil != init {
		ctx2, err := hbuf.CloneContext(ctx)
		if err != nil {
			tickerError(err)
			return
		}
		err = init(ctx2)
		hbuf.CloseContext(ctx2)
		if err != nil {
			tickerError(err)
			return
		}
	}
	if nil == call {
		return
	}
	isFast := true
	for {
		select {
		case <-ctx.Done():
			t.Stop()
			break
		case <-t.C:
			if isFast {
				t.Reset(duration)
				isFast = false
			}
			ctx2, err := hbuf.CloneContext(ctx)
			if err != nil {
				tickerError(err)
				return
			}
			err = call(ctx2)
			hbuf.CloseContext(ctx2)
			if err != nil {
				tickerError(err)
				return
			}
		}
	}
}

func tickerError(err error) {
	switch err.(type) {
	case *Error:
		err.(*Error).PrintStack()
	default:
		log.Println(err)
	}
}
func TickerDelay(ctx context.Context, delay time.Duration, duration time.Duration, call func(ctx context.Context) error, init func(ctx context.Context) error) {
	if 0 >= delay {
		_ = log.Output(2, fmt.Sprintln("TickerDelay:delay time cannot be less than 0"))
		return
	}
	if 0 >= duration {
		_ = log.Output(2, fmt.Sprintln("TickerDelay:Cycle time cannot be less than 0"))
		return
	}
	go ticker(ctx, time.NewTicker(delay), duration, call, init)
}

func TickerTime(ctx context.Context, t time.Time, duration time.Duration, call func(ctx context.Context) error, init func(ctx context.Context) error) {
	now := time.Now()
	if 0 >= t.Sub(now) {
		_ = log.Output(2, fmt.Sprintln("TickerTime:Start time cannot be less than current time"))
		return
	}
	if 0 >= duration {
		_ = log.Output(2, fmt.Sprintln("TickerTime:Cycle time cannot be less than 0"))
		return
	}

	go ticker(ctx, time.NewTicker(t.Sub(now)), duration, call, init)
}
