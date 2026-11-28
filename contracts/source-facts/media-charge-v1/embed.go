// Package mediachargev1 supplies the independent media-charge source contract.
package mediachargev1

import _ "embed"

const ID = "unirouter.media-charge.v1"

//go:embed schema.json
var schema []byte

//go:embed manifest.json
var manifest []byte

func Schema() []byte   { return append([]byte(nil), schema...) }
func Manifest() []byte { return append([]byte(nil), manifest...) }
