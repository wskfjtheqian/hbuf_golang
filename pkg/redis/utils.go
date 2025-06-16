package redis

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"github.com/wskfjtheqian/hbuf_golang/pkg/db"
	"github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func Set(ctx context.Context, key string, value any, duration time.Duration) error {
	marshal, err := json.Marshal(value)
	if err != nil {
		return erro.Wrap(err)
	}
	c := GET(ctx)
	err = c.Send("SET", key, string(marshal))
	if err != nil {
		return erro.Wrap(err)
	}
	if 0 < duration {
		err = c.Send("EXPIRE", key, strconv.Itoa(int(duration/time.Second)))
		if err != nil {
			return erro.Wrap(err)
		}
	}
	err = c.Flush()
	if err != nil {
		return erro.Wrap(err)
	}
	return nil
}

func Get[T any](ctx context.Context, key string, value *T) (*T, error) {
	reply, err := GET(ctx).Do("GET", key)
	if err != nil {
		return nil, erro.Wrap(err)
	}
	if nil == reply || 0 == len(reply.([]uint8)) {
		return nil, nil
	}
	err = json.Unmarshal(reply.([]uint8), value)
	if err != nil {
		return nil, erro.Wrap(err)
	}
	return value, nil
}

func Del(ctx context.Context, key string) error {
	c, ok := FromContext(ctx)
	if !ok {
		return erro.NewError("redis client not found in context")
	}
	c.Get().S(ctx, "DEL", key)

	reply, err := c.Get() .Do("KEYS", key).
	if err != nil {
		return erro.Wrap(err)
	}
	if nil == reply || 0 == len(reply.([]any)) {
		return nil
	}
	err = c.Send("DEL", reply.([]any)...)
	if err != nil {
		return erro.Wrap(err)
	}
	return c.Flush()
}

func Ping(ctx context.Context) error {
	c := GET(ctx)
	reply, err := c.Do("PING")
	if err != nil {
		return erro.Wrap(err)
	}
	if nil == reply || "PONG" != strings.ToUpper(reply.(string)) {
		return erro.NewError(reply.(string))
	}
	return nil
}

func SetNx(ctx context.Context, key string, value any, duration time.Duration) (bool, error) {
	marshal, err := json.Marshal(value)
	if err != nil {
		return false, erro.Wrap(err)
	}

	c := GET(ctx)
	reply, err := c.Do("SETNX", key, string(marshal))
	if err != nil {
		return false, erro.Wrap(err)
	}
	if nil == reply {
		return false, nil
	}
	if 0 < duration && 0 != reply.(int64) {
		_, err = c.Do("EXPIRE", key, strconv.Itoa(int(duration/time.Second)))
		if err != nil {
			return false, erro.Wrap(err)
		}
	}
	return 0 != reply.(int64), nil
}

func DbSet(ctx context.Context, key, dbName string, sql *db.Sql, value any, duration time.Duration) (string, error) {
	lock, err := DbLock(ctx, dbName)
	if err != nil || !lock {
		return "", err
	}
	if 0 == len(key) {
		key, err = createDbKey(dbName, sql)
		if err != nil {
			return "", erro.Wrap(err)
		}
	}
	return key, Set(ctx, key, value, duration)
}

func DbGet[T any](ctx context.Context, key, dbName string, sql *db.Sql, value *T) (*T, string, error) {
	if 0 == len(key) {
		var err error
		key, err = createDbKey(dbName, sql)
		if err != nil {
			return nil, "", err
		}
	}
	get, err := Get(ctx, key, value)
	if err != nil {
		return nil, "", err
	}
	return get, key, nil
}

func DbDel(ctx context.Context, dbName string) error {
	_, err := DbLock(ctx, dbName)
	if err != nil {
		return err
	}
	key := strings.Builder{}
	key.WriteString("db:")
	key.WriteString(dbName)
	key.WriteString(":")
	key.WriteString("*")
	return Del(ctx, key.String())
}

func DbLock(ctx context.Context, dbName string) (bool, error) {
	lock := strings.Builder{}
	lock.WriteString("db:cache:lock:")
	lock.WriteString(dbName)
	return SetNx(ctx, lock.String(), "DbLock", 0)
}

func DbUnlock(ctx context.Context, dbName string) error {
	lock := strings.Builder{}
	lock.WriteString("db:cache:lock:")
	lock.WriteString(dbName)
	return Del(ctx, lock.String())
}

func createDbKey(dbName string, sql *db.Sql) (string, error) {
	if nil == sql {
		return "", erro.NewError("create db key ,sql is nil")
	}
	pc, file, line, ok := runtime.Caller(2)
	if !ok {
		return "", erro.NewError("create db key error")
	}
	temp := strings.Builder{}
	temp.WriteString(file)
	temp.WriteString(":")
	temp.WriteString(strconv.Itoa(line))
	temp.WriteString(sql.ToText())
	data := md5.Sum([]byte(temp.String()))

	f := runtime.FuncForPC(pc)
	key := strings.Builder{}
	key.WriteString("db:")
	key.WriteString(dbName)
	key.WriteString(":")
	key.WriteString(f.Name())
	key.WriteString(":")
	key.WriteString(hex.EncodeToString(data[:]))
	return key.String(), nil
}
