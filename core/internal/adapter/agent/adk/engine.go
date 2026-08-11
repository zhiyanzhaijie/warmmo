package adk

import (
	"context"
)

const appName = "warmmo"

type ModelResolver func(context.Context, string, string) (ModelConfig, error)
