# 저장과 복구 구조

[문서 목록](README.md) · [Profile 호환성](profiles.md#저장-형식-업그레이드와-구버전-실행) · [백업과 복구](backup.md)

이 문서는 저장 계층을 수정할 때 유지해야 하는 경계를 설명한다.
사용자 데이터는 CLI를 통해 변경한다. 파일이 없거나 읽히지 않는다는 이유로
저장소를 초기화하거나 잠금·권한 검사를 생략하지 않는다.

## 소유 경계

`internal/platformfs`는 private 파일·디렉터리, 열린 경로의 동일성과 containment를 검사한다.
Unix 권한과 Windows ACL·handle의 차이는 이 계층에서 처리한다.
Windows AppContainer의 생성·검증은 같은 사용자·SYSTEM·해당 컨테이너 SID 정책을 사용한다.
지원하는 경로와 플랫폼 제약은 [Windows 안내](windows.md#storage-and-processes)를 따른다.

논리 profile 이름과 물리 파일명은 분리한다. `internal/profilekey`가 고정 길이 key와
기존 파일의 해석을 담당한다. 이름 길이를 파일시스템 component 제한에 맞춰 줄이거나
새 경로와 과거 경로를 별개 profile로 해석하지 않는다.

## 잠금과 교체

`internal/maintenance`의 data-root gate를 먼저 획득하고 domain/profile 잠금을 획득한다.
일반 접근은 shared gate, 여러 저장 영역을 교체하는 restore·migration은 exclusive gate를 사용한다.
shared 상태에서 복구가 필요하면 잠금을 놓고 exclusive로 다시 검사한다.
동일 잠금의 승격을 기다리며 교착시키지 않는다. 잠금 대기는 호출 context의 취소를 따른다.

Linux/macOS의 `env list`, `variable list/get`(`var`), `secret list`(`sec`)는
`AcquireQuery`로 기존 `lock.turnstile`과 `lock`을 읽기 전용으로 열고 같은 shared flock을 잡는다.
기존 저장소가 정상이고 복구할 transaction이 없으면 쓰기 접근 없이 조회한다.
잠금 누락은 기존 `AcquireShared`의 초기화 경로로, pending/orphan은 잠금을 놓은 뒤
기존 exclusive 재검사·복구 경로로 들어간다. 쓰기가 막히면 조회도 실패한다.
읽기·권한·소유권·no-follow 검사 실패를 빈 저장소나 초기화 조건으로 해석하지 않는다.
공개 task/workstream/validation 조회도 전역 `AcquireQuery`를 사용한다.
프로필은 `LockQueryShared`로 기존 turnstile → gate를 O_RDONLY로 열며 같은 writer와 경합한다.
프로필 잠금 누락에만 기존 초기화 경로로 전환한다. 전역 → 프로필 순서를 유지한다.
단일 조회·cursor read는 캐시 생성·정리를 하지 않는다. 새 page/graph publication만
기본 캐시나 명시적 private `--query-cache-dir`에 쓰며, 성공 후 같은 디렉터리의
인식 가능한 만료 snapshot만 metadata 범위 내에서 판독·정리한다.
명시 캐시는 data root와 분리하며 root/parent guard를 읽기와 쓰기에 모두 적용한다.
cursor continuation은 현재 data root·잠금·WAL 없이 고정 revision을 읽는다.
Windows와 다른 조회·변경·실행·dashboard 및 task의 일반 Read API는 기존 shared gate 계약을 유지한다.

관리 포트 레지스트리의 일반 쓰기는 data-root shared gate를 먼저 잡고
`registry.lock`으로 직렬화한다. 이미 data-root gate를 잡은 정리·복원은
레지스트리 잠금으로 바로 진행한다. 조회는 준비된 gate를 읽기 전용으로 사용하며
중단된 저장소 변경이 있으면 복구가 필요하다고 보고한다. 기존 포트 전용 저장소의
최초 조회는 gate를 만들지 않는다.

프로세스 등록과 managed/project 수명 주기 작업은 실행 기록이나 요청
영수증을 쓰기 전에 `.execution/lock`의 shared lock을 얻는다. 같은 root의
동기 하위 작업은 호출자가 이미 얻은 lock을 사용한다. 실행 변경을 막는
exclusive lock은 data-root maintenance gate보다 먼저 얻어야 한다. 실행의
준비 과정에서 변수 조회와 포트 할당이 data-root gate를 사용하기 때문이다.

프로필 이름 변경 저장소 트랜잭션은 원본과 대상의 값 주소, task head,
포트 레지스트리, `profile-renames/names.json`, 요청 영수증을 함께 게시한다.
이름 예약은 각 이전 주소를 같은 소유자와 현재 이름에 연결한다.
값 프로필은 주소를 다시 서명하며 기존 프로필·키·값 버전과 암호문을 유지한다.
task는 같은 이력과 영수증을 가진 검증된 generation을 먼저 준비하고
트랜잭션 안에서 head를 바꾼다. 종료된 실행의 원본 기록은 기존 이름으로 검증한다.
이전 이름으로 일반 쓰기가 들어오면 현재 이름을 알려주고, 재시도나 보관 복원은
저장된 원래 이름과 소유권을 확인한다.

Protocol 25의 독립 실행과 이미 열린 쓰기 핸들은 새 입장 잠금에 참여하지 않는다.
첫 이름 변경 적용은 이전 버전의 writer가 이 데이터에 접근하는 경로를 닫는다.

전환은 data root와 보호된 값 전달의 경로 인증을 유지한다. 기존
`.maintenance/restore-pending.json`에는 구버전이 거절하는 형식의 차단 포인터를
남기고, 현재 rollback과 암호화 forward 복구는 별도 포인터를 사용한다.
포트·프로세스·프로젝트·네트워크·proxy·공유의 기존 경로는 차단 파일로
닫으며, 현재 버전은 해당 도메인을 `.runtime-v26`에서 읽는다.
격리된 macOS와 Linux에서 구버전의 대표 쓰기 6종이 차단된 것을 확인했다.
Linux에서는 발행 잠금의 파일 핸들을 이미 연 요청도 자식 실행과 기록을
남기지 않았다. 전환은 기존 경로를 닫기 전과 게시 전에 런타임 쓰기 잠금을
검사한다. 중단된 전환은 원래 요청 UUID와 digest로 재개한다. 옮기는 도중
구버전이 기존 디렉터리를 다시 만들면 양쪽 데이터를 보존하고 전환을 중단한다.

여러 파일을 바꾸는 작업은 복구 가능한 transaction으로 게시한다.
저장 성공 응답 이전에 필요한 flush와 commit 경계를 확인하며,
응답이 실패했다고 디스크 변경이 없었다고 가정하지 않는다.
pending transaction은 domain 접근 전에 복구하거나 명시적인 복구 상태로 차단한다.
암호화 저장소는 해제·인증이 필요한 별도 복구 경로를 사용하고 평문 before-image를 만들지 않는다.
키 준비와 세션 수명 규칙은 [var/sec 계약](var-sec-contract.md)을 따른다.

## Task journal

`internal/tasks`의 물리 저장 형식 v3는 profile별 세대와 head, WAL, materialized snapshot을 사용한다.
논리 이벤트 형식과 물리 저장 형식은 독립적이다. WAL frame의 리비전 연속성,
event sequence, 요청 영수증·context 참조를 검증하고 commit한 변경을 반영한다.
snapshot은 현재 조회를 위한 materialized state이며 이력을 삭제하는 retention 수단이 아니다.

기존 journal의 전환과 최초 변경은 하나의 복구 가능한 게시 경계로 처리한다.
조회·미리보기를 변경으로 해석하지 않는다. backup/restore/cleanup은 내부 파일을
임의 조합하지 않고 task 저장 계층의 논리 snapshot 경계를 사용한다.
오래된 writer의 거절과 복구 절차는 [Profile 호환성](profiles.md#저장-형식-업그레이드와-구버전-실행)을 따른다.

## Process 기록과 로그

`internal/services`는 active record, 정상 보관된 execution, 누락과 손상을 구분한다.
보관 marker 또는 유효한 과거 archive proof로 정상 보관을 판정한다.
record가 없다는 사실만으로 빈 execution이나 정상 정리로 처리하지 않는다.
실행 소유권과 lease를 확인하며 저장된 PID만으로 프로세스를 종료하거나 중복 시작하지 않는다.

capture 로그는 private 파일에 append하고 물리 상한에서 compact한다.
조회에는 마지막 1 MiB를 제공하며 영구 감사 로그로 사용하지 않는다.
secret 마스킹은 저장 전 적용한다. 보관과 정리의 사용자 절차는
[프로세스](processes.md), [정리와 보관함](cleanup.md), [출력 보호](output-protection.md)를 따른다.

## 오류와 검증

저장 오류는 공통 [CLI 오류 계약](cli-output.md)을 사용한다. 공개 진단에는
domain·operation·resource와 안전한 OS 오류 정보를 보존하고 비밀 원문을 넣지 않는다.
손상을 빈 목록으로 바꾸거나 접근 거부를 자동 권한 완화로 해결하지 않는다.

변경 시에는 원자 교체 전·후 실패, 응답 유실 후 같은 요청의 재시도,
대기 중 취소, 동시 reader/writer, 구형 저장 형식의 전환과 거절을 확인한다.
플랫폼 API 경계는 [native 검증](development.md#varsec-통합-수용-검증),
설치된 CLI의 복구는 [통합 검증](../verify/README.md)을 사용한다.
