# B02 수명 연장 없는 인증용 cached inspection 리뷰

사용처/현재 입력 비교를 위해 이미 warm인 key만 빌리는 내부 inspect RPC를 추가했다. 일반 Use와 달리 native unwrap·KDF·자동 재해제·touch를 수행하지 않는다. 파일의 key-slot fingerprint/수동 latch/만료를 기존 read로 재검사하고 cold/manual/recovery 상태는 거절한다. 반환 Material과 wire key copy는 기존 소유·Destroy/clear 규칙을 따른다. public CLI 출력이나 launch ticket을 추가하지 않는다.

| 영역 | 최종 상태·근거 |
| --- | --- |
| warm-only·idle·factory 호출 | reviewed/pass, fake native adapter의 cold/expired/manual inspect에서 unwrap factory 미호출, warm key 동일·IdleExpiresAt 불변 |
| 실제 daemon IPC | reviewed/pass, 별도 process의 cold 거절→명시 unlock→3회 inspect→deadline 불변→manual lock 거절 |
| 잠금 경합·old hello | reviewed/pass, inspect도 admission delivery counter에 포함, key response를 실제 connection wrapper에서 막은 동안 lock ack 금지·drain 뒤 완료, old epoch 거절 |
| peer/framing/응답·키 수명 | reviewed, 기존 양방향 UID/SID/vault/session/challenge/frame/connection 한도 재사용, inspect 외 비-use에 key 금지 유지, 32 bytes·ticket 없음 검사와 clear |
| 호환성·스토리지·호출자 | reviewed/pass, 새 private operation은 SessionProtocol 3, stable control retirement는 유지, StorageVersion/HKDF 1 golden 검사 pass. 아직 public metadata consumer에는 연결하지 않아 B02 전체 미완료 |

전체 vault race pass(22.494s), 대상 inspection/drain/StorageV1 race pass(1.694s). Windows x64/ARM64와 Darwin ARM64 compile 및 vet를 수행한다. 이는 native 통과의 대체가 아니며 후속 native stage에서 다시 확인한다. 상태 조회용 consumer는 Manager.Client로 worker를 시작하지 않고 기존 OpenSessionClient에만 연결하도록 다음 하위 작업에서 구현한다. 구 protocol의 passive inspection은 pending으로 남기며 worker retirement는 실제 값 작업의 기존 upgrade 경로에서만 수행한다.

보안 리뷰는 untrusted endpoint/frame → peer/challenge/epoch 검증 → admission → cached Material → bounded private response → caller clear를 추적했다. 같은 UID 앱에 대한 sandbox는 기존 보장 밖이며 Keychain ACL·native provider 보호 범위는 변경하지 않았다. 이번 하위 작업 범위에 남은 적격 결함은 없다. native/사용처 표시/actual slot 비교/cache consumer는 후속 B02/C01 검증으로 남긴다.
