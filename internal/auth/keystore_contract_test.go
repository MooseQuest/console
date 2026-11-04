package auth_test

import (
	"testing"

	"github.com/moosequest/console/internal/auth"
	"github.com/moosequest/console/internal/auth/authtest"
)

func TestMemKeyStoreContract(t *testing.T) {
	authtest.RunKeyStoreContract(t, func(*testing.T) auth.KeyStore { return auth.NewMemKeyStore() })
}
