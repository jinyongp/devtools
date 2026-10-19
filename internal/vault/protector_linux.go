package vault

func nativeProtector(backend, id string, interactive bool) (Protector, error) {
	if backend != "secret-service" {
		return nil, ErrUnavailable
	}
	return SecretServiceProtector{VaultID: id, AllowInteraction: interactive}, nil
}
