package hcache

import "context"

type DBCache struct {
}

func (d *DBCache) Get(ctx context.Context, table string, sql string, out any) (bool, error) {

	return false, nil
}

func (d *DBCache) Set(ctx context.Context, table string, sql string, in any) error {

	return nil
}

func (d *DBCache) Clear(ctx context.Context, table string) error {

	return nil
}
