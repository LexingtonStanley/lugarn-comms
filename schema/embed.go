// Package schema provides the broker's versioned, embedded wire schemas.
package schema

import "embed"

// Files contains the four v1 schemas, without examples.
//
//go:embed *.v1.schema.json
var Files embed.FS
