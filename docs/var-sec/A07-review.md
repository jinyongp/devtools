# A07 실행 입력 snapshot 검토

범위: vault의 일관된 profile/keyset 읽기, values의 선택된 실행 view,
execution의 ResolvePlan → AuthenticateSnapshot → Materialize와 민감 payload,
부모 환경·OS 입력 한도·실행 경계. launcher admission/재시작 handoff는 A08,
출력 redactor는 A09, migration·전체 CLI 전환은 A10/A11이다.

| 영역 | 상태 | 근거 |
| --- | --- | --- |
| 원자적 인증 읽기 | reviewed | root acquisition은 gate 밖 1회; 하나의 shared disk gate에서 catalog·profile ID/generation·wrapped key·manifest 검증 |
| 실제 값 선택 | reviewed | common/env·빈 override·만료 override 무대체, binding/등록-only는 secret 복호화 생략; manifest 변조는 여전히 거절 |
| 준비·물질화 경계 | reviewed | command/parent 복사, provider의 profile/env·주입 가능 여부 검증, 30초 준비/slot expiry 검사 |
| 실제 payload | reviewed | argv/env/resolved executable/readiness/patterns를 소유 byte buffer로 고정; launcher용 명시 encode/decode; fmt/JSON 거절 |
| 부모 환경 | reviewed | inherit 기본, clean/allowlist는 별도 정책, 관리 context 미상속, Windows SYSTEMROOT는 Go가 추가하기 전에 고정 |
| preflight | reviewed | 버전 도구에는 전체 주입값 미전달, 알려진 secret과 control context를 parent probe에서도 제거; 필요한 PATH만 기존 경계에서 사용 |
| OS 크기·표현 | reviewed | Linux stack/page/단일 문자열·Darwin kern.argmax·Windows UTF-16/명령줄, 값 없는 초과 오류와 16MiB transport 상한 |
| 실행 소비자 | reviewed | 실제 child가 저장/호출자 변경 전의 값으로 실행됨; public compatibility field 변경도 frozen payload를 바꾸지 못함 |
| codec·공개 메타데이터 | reviewed | ID/version으로만 effective revision 계산, root key epoch 제외; case alias/duplicate/unknown 필드 거절 |
| 회귀·플랫폼 | reviewed local/compile | 전체 Go suite, 영향 영역 race/vet, 5개 cross target build/vet; 실제 native Actions는 N01–N04/A12에 남음 |

실제 저장소에서 10/100/1000개 값을 읽고 root acquisition이 1회임을 검사했다.
이는 OS keyring을 1000번 직접 호출한 실험이 아니라, 인증된 catalog/keyset/AEAD와
consumer view를 함께 통과하는 실제 저장 경계 검사다. 동시 bulk commit의 짝 혼합 없음,
빈 값·만료 override·변조, metadata-only 및 원문 없는 오류를 확인했다.
별도 실제 프로세스에서도 snapshot 뒤 저장 값/parent/호출자 argv를 변경한 영향을
받지 않음을 확인했다. 테스트 값과 디렉터리는 모두 합성·격리 자원이다.

리뷰에서 수정한 점:

- metadata-only snapshot이나 다른 profile의 snapshot이 주입에 쓰이지 않도록
  consumer 쪽에서도 profile/env와 주입 가능 여부를 검증했다.
- 포트 표현식도 캡처한 parent 환경을 사용하게 하고, Windows 필수 SYSTEMROOT를
  spawn 직전 재조회하지 않도록 준비 단계에서 고정했다.
- decode/expiry/해제 뒤에는 원래 값 재조회나 parent 환경 fallback으로 진행하지 않는다.
- Go JSON decoder의 case-folded field alias가 닫힌 schema를 우회하지 않도록
  exact field를 검사한다. 동적 map의 대소문자 구분은 유지한다.
- 기존 config의 nil port range를 빈 range로 바꾸지 않도록 clone을 수정했다.

Windows Unicode 환경 블록 전체를 32767자로 제한하지 않는다. 해당 한도는 명령줄과
개별 변수에 적용되며, 전체 입력은 메모리/전송 예산으로 별도 제한한다.
Unix 검사는 pointer와 실행 파일/interpreter용 8KiB 여유를 둔 보수적 preflight다.
OS와 개별 프로그램의 모든 시작 실패를 사전에 없앤다는 뜻은 아니다.
근거: Microsoft [CreateProcessW](https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-createprocessw),
[Environment Variables](https://learn.microsoft.com/en-us/windows/win32/procthread/environment-variables),
Linux [exec.c](https://github.com/torvalds/linux/blob/master/fs/exec.c).

`SnapshotMetadata.Slots`는 source 의존성 메타데이터이며 앱의 getenv 사용량이 아니다.
전달/사용처 공개 계약은 B02에서 구분한다. Legacy private snapshot은 migration 전
호환 경로이며 `integrity_verified:false`다. public-only 비밀 없는 profile도 cryptographic
value integrity를 검증했다고 표시하지 않는다. 새 sealed profile의 공개 var는 전체
manifest 검증에 포함된다. 사용자 저장소는 이 단위 작업에서 변경하지 않았다.

검증 명령: `go test ./...`, 영향 execution/ports/doctor/process/vault/values race/vet,
Windows amd64/arm64·Darwin amd64/arm64·Linux arm64 CLI build와 관련 vet/test compile.
Raw log는 구현 세션의 `/tmp/devtools-values-plan/A07-*.log`에 보존했다.
