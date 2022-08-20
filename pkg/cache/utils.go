package cache

import (
	"context"
	"encoding/json"
	"math/rand"
	"strconv"
)

func CacheSet(ctx context.Context, key string, value any) error {
	marshal, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	c := GET(ctx)
	err = c.Send("SET", key, string(marshal))
	if err != nil {
		return nil
	}
	e := rand.Intn(3000-2000) + 2000
	err = c.Send("EXPIRE", key, strconv.Itoa(e))
	if err != nil {
		return err
	}
	return c.Flush()
}

func CacheGet[T any](ctx context.Context, key string, value *T) (*T, error) {
	reply, err := GET(ctx).Do("GET", key)
	if err != nil {
		return nil, err
	}
	if nil == reply || 0 == len(reply.([]uint8)) {
		return nil, nil
	}
	err = json.Unmarshal(reply.([]uint8), value)
	if err != nil {
		return nil, err
	}
	return value, nil
}

func CacheDel(ctx context.Context, key string) error {
	c := GET(ctx)
	reply, err := c.Do("KEYS", key)
	if err != nil {
		return err
	}
	if nil == reply || 0 == len(reply.([]any)) {
		return nil
	}
	err = c.Send("DEL", reply.([]any)...)
	if err != nil {
		return err
	}
	return c.Flush()
}
