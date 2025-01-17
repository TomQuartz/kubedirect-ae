package scaler

import (
	"context"
)

type Scaler interface {
	Scale(ctx context.Context, key string, desired int) error
}
