package main

import (
	"context"
)

// DataStore defines the interface for any database implementation.
// This allows switching between MongoDB, PostgreSQL, etc.
type DataStore interface {
	Create(ctx context.Context, collection string, data interface{}) (string, error)
	Find(ctx context.Context, collection string, filter interface{}) ([]map[string]interface{}, error)
	Update(ctx context.Context, collection string, filter interface{}, update interface{}) error
	Delete(ctx context.Context, collection string, filter interface{}) error
}
