# A05 Linux Secret Service 구현 검토

범위: user D-Bus transport, Secret Service adapter, private-bus integration test.
실제 OS keyring 가용·잠금·로그아웃 검증은 N03이며, WSL 재시작은 N04/A12다.

| 영역 | 상태 | 근거 |
| --- | --- | --- |
| transport 신뢰 경계 | reviewed | single Unix socket만 허용, SO_PEERCRED UID 확인, EXTERNAL 인증, unique service owner UID 고정 |
| 키 보존·lookup | reviewed | vault별 lookup attribute, duplicate 거절, CreateItem replace=false, missing key 자동 생성 없음 |
| 세션·오류 | reviewed | 연결마다 OpenSession, 제한 시간 뒤 close, locked/취소/서비스 부재/NoSession 구분, 임의 backend fallback 없음 |
| UI·prompt | reviewed | 기본 비대화형, explicit interaction만 Unlock/Prompt, sender/path/interface 확인한 Completed, dismissed→denied |
| payload·자원 | reviewed | 로컬 slot은 locator뿐, OS payload bound 검증, secret bytes 정리, 5초 auth/30초 operation timeout |
| 실제 wire·동시성 | reviewed | disposable dbus-daemon 위 fixture service를 통한 코드·직렬화·인증·상태 오류 검사, race 세 번 반복 |
| 실제 GNOME/KWallet·종료/잠금 | pending N03 | fixture는 실제 keyring 저장 암호화 검증이 아님; opt-in native test 별도 |

리뷰에서 Unlock이 prompt도 unlocked item도 반환하지 않은 경우 성공으로 수용하지
않도록 수정했다. D-Bus 오류의 value/pointer 표현을 모두 지원하고 취소된 dial/auth는
context 오류로 보존한다. UI 취소와 서비스 세션 상실을 실제 private bus에서 재검사했다.

전송은 Secret Service 표준 `plain` session이다. 이는 저장 평문을 뜻하지 않으며
로컬 user bus의 OS 사용자 경계를 신뢰한다. 별도 D-Bus 세션 암호화나 동일 사용자
악성 프로세스 격리를 보장하지 않는다. TCP·원격/대체 transport·자동 bus/service 시작은
사용하지 않는다. 제품이 dbus-daemon·keyring CLI를 설치하거나 호출하지 않는다.
테스트만 폐기 가능한 dbus-daemon을 시작하며 실제 사용자 keyring은 건드리지 않았다.

의존성: `github.com/godbus/dbus/v5 v5.2.4`, BSD-2-Clause, Go 1.20 이상,
기존 x/sys만 하위 의존성으로 요구한다. 표준 Go에는 D-Bus codec/auth가 없으므로 채택했다.
공식 [릴리스](https://github.com/godbus/dbus/releases/tag/v5.2.4)와
[module 정보](https://proxy.golang.org/github.com/godbus/dbus/v5/@v/v5.2.4.info),
라이선스·공식 Go vulnerability module index를 확인했다. 확인 시 module 항목이 없었지만
그 사실은 라이브러리 전체 보안 감사의 결과가 아니다.
API 기준은 [freedesktop Secret Service](https://specifications.freedesktop.org/secret-service/latest/).

검증: vault/values/execution race, vault vet, Linux amd64·arm64 compile 및 Darwin/Windows
기존 adapter compile. N03 실제 OS 수용 조건은 Actions의 disposable Linux keyring에서
추가 검증해야 하며 현재 결과로 완료 처리하지 않았다.
