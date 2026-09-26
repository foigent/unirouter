// Package predispatchedv2 embeds the independent pre-dispatch terminal source contract.
package predispatchedv2

import _ "embed"

const SchemaVersion = "unirouter.source-observation.v2"

//go:embed schema.json
var schema []byte

//go:embed manifest.json
var manifest []byte

//go:embed golden-observations.json
var vectors []byte

func Schema() []byte   { return append([]byte(nil), schema...) }
func Manifest() []byte { return append([]byte(nil), manifest...) }
func Vectors() []byte  { return append([]byte(nil), vectors...) }
