package sql

import "context"

type Cache interface {
}

func GetCache(ctx context.Context, tableName string, builder Builder) {

}

func SetCache(ctx context.Context, tableName string, builder Builder) {

}

func ClearCache(ctx context.Context, tableName string) error {
	return nil
}
