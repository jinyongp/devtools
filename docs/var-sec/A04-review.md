# A04 Windows 사용자 DPAPI 구현 검토

범위: `internal/vault/dpapi_windows.go`, native test 및 공통 OS payload 검증.
실제 Windows x64·ARM64·다른 SID·설치/업데이트 검증은 N02의 필수 작업이다.

| 영역 | 상태 | 근거 |
| --- | --- | --- |
| 사용자 범위·UI | reviewed | 기존 x/sys/windows 직접 API, UI_FORBIDDEN만 지정, LOCAL_MACHINE·prompt 없음 |
| vault/형식/키 binding | reviewed | optional entropy에 vault ID, 보호 payload에 도메인·버전·backend·ID·32-byte key와 checksum 포함 |
| 입력·오류·자원 | reviewed | 입력/출력 크기 제한, 취소 전후 확인, 원시 OS 오류·키 미출력, LocalFree 전 plaintext 정리 |
| 호출자·저장 경계 | reviewed | KeyFileStore의 backend/ID pinning과 실제 unwrap 확인을 그대로 소비; 기존 platformfs private ACL 유지 |
| payload 변조 | reviewed | 모든 byte 변조·다른 vault/backend·잘림을 Linux에서 실제 공통 parser로 거절 |
| Windows 컴파일 | reviewed | CGO_ENABLED=0, Windows amd64·arm64 test binary 컴파일 |
| 실제 DPAPI·SID·native ACL·설치 | pending N02 | 현재 Linux에서 실행할 수 없음; native test는 작성했으나 여기서 실행하지 않음 |

검토 중 OS 복호화 성공만으로 키를 수용하지 않도록 typed payload와 checksum을
추가했다. DPAPI의 손상 오류 코드는 일정하지 않으므로 다른 사용자와 손상을
항상 별도 원인으로 판별한다고 약속하지 않는다. access denied와 UI restriction은
별도 안전한 오류로 반환하며 일반 unwrap 실패는 integrity 오류다. 자동 fallback은 없다.

근거: [Microsoft CryptProtectData](https://learn.microsoft.com/en-us/windows/win32/api/dpapi/nf-dpapi-cryptprotectdata),
[CryptUnprotectData](https://learn.microsoft.com/en-us/windows/win32/api/dpapi/nf-dpapi-cryptunprotectdata).
검증: Linux vault/values/execution race, vault vet, Windows x64·ARM64 cross-compile.
실제 사용자 저장소나 키를 읽거나 변경하지 않았다. runtime 통합은 A11까지 후속이다.
