// Package sourcefactsv1 embeds the UR-owned candidate source contract bytes.
package sourcefactsv1

import _ "embed"

const SchemaVersion = "unirouter.source-fact.v1"

//go:embed source-fact.schema.json
var schema []byte

//go:embed manifest.json
var manifest []byte

func Schema() []byte   { return append([]byte(nil), schema...) }
func Manifest() []byte { return append([]byte(nil), manifest...) }
