package vault

func nativeProtector(backend, id string, interactive bool) (Protector, error) {
	if backend != "dpapi" {
		return nil, ErrUnavailable
	}
	return DPAPIProtector{VaultID: id}, nil
}
