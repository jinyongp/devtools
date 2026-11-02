# Storage/query/init 작업의 검토 기록

Workstream `76536172-4285-4efa-9ad6-528d3fa1de5f`의 구현 범위를 검토했다.
최종 플랫폼 결과와 source 기준은 V08·V09·V10·V14·V15 validation 기록을 정본으로 사용한다.
이 파일은 검토 범위·수정 이유·유지한 경계를 남기며 실행 중인 CI를 통과로 간주하지 않는다.

| 범위 | 확인한 계약과 근거 |
| --- | --- |
| 원래 doctor 차이와 유사 조회 | 기존 pair/single-file writer 잠금을 유지하면서 O_RDONLY 관측을 사용한다. Linux 실제 write denial, 양방향 writer 차단·취소·inventory 불변을 확인했다. 원래 사용자 macOS의 syscall과 당시 저장소 상태는 확인되지 않았다. |
| 값 입력·revision·bundle·receipt | 일반 실행은 current-only다. Doctor/project/process의 nested 실패는 typed diagnostic을 보존하며 미인증 상태를 정상으로 바꾸지 않는다. 설치 RW/RO 비교와 package race를 수행했다. |
| profile·task·handoff·runtime·logs | Held 경계의 재진입과 nested lock을 점검했다. Query 준비 실패와 runtime lease 준비 remedy를 구분했다. Handoff 집계는 인증 완료 전 부분 숫자를 제공하지 않는다. 실제 busy/idle, RPC 불가, redacted captured logs를 검사했다. |
| 명시적 전환·복구 | 기존 데이터와 logical identity/revision/replay를 유지한다. Auth를 gate 밖에서 얻고 기존 idle·key-slot identity를 writer gate 아래 재검증한다. Public/task before-image와 보호된 forward recovery를 구분하며 강제 종료 후 재개를 검사했다. |
| init | 기존 local config, 실제 basename, namespace 중복, 명시적 공유, terminal/JSON/agent/CI 분기를 검토했다. 읽기 전용 data와 writable project, 부모 설정·이동·symlink, 취소·EOF·Ctrl-C·잘못된 입력, 결과 전송 실패 뒤 재실행을 검사한다. |
| 설치 업데이트 | 새 executable의 같은 coordinator를 사용한다. Same-image backlog와 locked vault 보류·명시 unlock 뒤 재개, 미래 형식 보존, binary/data 결과·exit/effects를 구분했다. Data commit 뒤 binary rollback을 수행하지 않는다. |
| 계약·문서·개인정보 | CLI protocol 22·envelope 1과 strict 소비자를 맞췄다. Repository의 기존 외부 프로젝트 식별자를 일반화/제거했고 hidden/ignored 텍스트를 다시 검색했다. 값·키·task context는 공유 검증 증거에 넣지 않는다. |

검토·재검증 과정에서 수정한 사항:

- Profile/task의 nested query가 writable lock을 다시 열던 경로와 준비 오류 안내를 수정했다.
- 잠긴 handoff summary에 추가된 안전한 진단을 설치 fixture와 strict schema에 반영했다.
- Protocol 변경 뒤 남아 있던 schema const·workflow assertion과 필수 profile 가정의 EOF 검사를 갱신했다.
- 미완료 Unix update의 오류를 다시 읽을 때 exit 3이 소실되던 문제와 변경 효과 보고를 수정했다.
- 구버전을 명시해 설치하는 PowerShell bootstrap은 해당 binary의 기존 응답 계약을 협상한다.
  이 호환성은 일반 values/task reader의 legacy 해석과 별개다. Protocol 22 후보는 storage 결과를 반드시 제공한다.
- macOS fixture의 Linux 전용 sleep 경로와 ConPTY가 trailing space를 cursor 이동으로 내보내는 경우를 수정했다.

유지한 범위:

- 현재 task container의 논리적 이력과 portable archive import codec을 보존한다.
  일반 조회·변경은 과거 물리 schema를 해석하거나 자동 변환하지 않는다.
- Init의 존재 여부는 선택 data root의 공개 identity/runtime metadata 기준이다.
  임의 project 디렉터리를 검색하거나 이름을 예약하는 registry를 추가하지 않는다.
- 새 vault backend 선택·자동 unlock·이전 writer 강제 종료·실사용 저장소 전환·설치 CLI 교체·릴리스는 수행하지 않았다.
- Native Windows의 기존 handle/lease protocol을 유지한다. Unix read-only flock 결과를 Windows의 read-only 정책 보장으로 확대하지 않는다.
