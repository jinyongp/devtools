package services

import (
	"context"
	"errors"
	"os"
)

func (s Store) serve(ctx context.Context, id string, inherited *os.File, execute Execute) error {
	return errors.ErrUnsupported
}
