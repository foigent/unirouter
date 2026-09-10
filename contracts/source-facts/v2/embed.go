// Package sourcefactsv2 embeds the independently versioned B observation contract.
package sourcefactsv2

import _ "embed"

const SchemaVersion = "unirouter.source-observation.v1"
const DispatchOutboxContract = "unirouter.source-dispatch-outbox.v1"

//go:embed source-observation.schema.json
var schema []byte

//go:embed manifest.json
var manifest []byte

//go:embed golden-observations.json
var vectors []byte

func Schema() []byte   { return append([]byte(nil), schema...) }
func Manifest() []byte { return append([]byte(nil), manifest...) }
func Vectors() []byte  { return append([]byte(nil), vectors...) }
