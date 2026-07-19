//go:build !linux

package snapshotpublish

func prepare(string) (Stage, error) {
	return nil, ErrUnsupported
}
