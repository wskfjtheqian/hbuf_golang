package utl

//
//import (
//	"context"
//	"fmt"
//	err2 "github.com/wskfjtheqian/hbuf_golang/pkg/erro"
//	"github.com/wskfjtheqian/hbuf_golang/pkg/rpc"
//	"log"
//	"time"
//)
//
//func ticker(ctx context.Context, t *time.Ticker, duration time.Duration, call func(ctx context.Context) error, init func(ctx context.Context) error) {
//	if nil != init {
//		ctx2, erro := rpc.CloneContext(ctx)
//		if erro != nil {
//			tickerError(erro)
//			return
//		}
//		erro = init(ctx2)
//		rpc.CloseContext(ctx2)
//		if erro != nil {
//			tickerError(erro)
//			return
//		}
//	}
//	if nil == call {
//		return
//	}
//	isFast := true
//	for {
//		select {
//		case <-ctx.Done():
//			t.Stop()
//			break
//		case <-t.C:
//			if isFast {
//				t.Reset(duration)
//				isFast = false
//			}
//			ctx2, erro := rpc.CloneContext(ctx)
//			if erro != nil {
//				tickerError(erro)
//				return
//			}
//			erro = call(ctx2)
//			rpc.CloseContext(ctx2)
//			if erro != nil {
//				tickerError(erro)
//				return
//			}
//		}
//	}
//}
//
//func tickerError(erro error) {
//	switch erro.(type) {
//	case *err2.Error:
//		erro.(*err2.Error).PrintStack()
//	default:
//		log.Println(erro)
//	}
//}
//
//func TickerDelay(ctx context.Context, delay time.Duration, duration time.Duration, call func(ctx context.Context) error, init func(ctx context.Context) error) {
//	if 0 >= delay {
//		_ = log.Output(2, fmt.Sprintln("TickerDelay:delay time cannot be less than 0"))
//		return
//	}
//	if 0 >= duration {
//		_ = log.Output(2, fmt.Sprintln("TickerDelay:Cycle time cannot be less than 0"))
//		return
//	}
//	go ticker(ctx, time.NewTicker(delay), duration, call, init)
//}
//
//func TickerTime(ctx context.Context, t time.Time, duration time.Duration, call func(ctx context.Context) error, init func(ctx context.Context) error) {
//	now := time.Now()
//	if 0 >= t.Sub(now) {
//		_ = log.Output(2, fmt.Sprintln("TickerTime:Start time cannot be less than current time"))
//		return
//	}
//	if 0 >= duration {
//		_ = log.Output(2, fmt.Sprintln("TickerTime:Cycle time cannot be less than 0"))
//		return
//	}
//
//	go ticker(ctx, time.NewTicker(t.Sub(now)), duration, call, init)
//}
