package vault

func nativeProtector(backend, id string, interactive bool) (Protector, error) {
	if backend != "keychain" {
		return nil, ErrUnavailable
	}
	return KeychainProtector{VaultID: id, AllowInteraction: interactive}, nil
}
