# devtools 문서

프로젝트에서 devtools를 처음 사용한다면 [한국어 README](../README.ko.md)의 설치와 첫 실행부터 시작한다.
[English README](../README.md)도 같은 흐름을 제공한다. 이 문서에서는 사용 목적에 맞는 안내를 찾을 수 있다.

## 문서 버전과 예제 읽기

이 checkout은 Windows 네이티브 지원을 포함한 **v0.25.0 배포 대상**을 설명한다.
v0.25.0 게시 전에는 검증된 후보로 사용한다.
게시된 릴리스와 소스 후보의 기능을 구분하고, 설치한 버전의 계약은 다음 명령으로 확인한다.

```sh
devtools version
devtools --help
devtools schema command run
```

현재 소스의 CLI protocol은 7, JSON envelope의 `schema_version`은 1이다.
`devtools.toml`에는 버전 필드를 넣지 않는다. 이전 릴리스와의 차이는 [출력 계약](cli-output.md)에 있다.
Windows 자산의 게시 여부·지원 목표·검증 한계는 [Windows 안내](windows.md)를 따른다.
과거 계획의 완료 체크나 primitive probe를 현재 제품의 검증 결과로 해석하지 않는다.

예제를 실행할 때는 다음 전제를 확인한다.

- **셸:** `sh` 코드 블록의 `$NAME`, `$(...)`, `\` 줄 연결과 리다이렉션은 POSIX 셸용이다.
  PowerShell에서는 `$env:NAME`, `[guid]::NewGuid().ToString()`과 해당 셸의 문법을 사용한다.
  CLI 명령 자체가 같아도 실행할 `sh`, `python3`, `pnpm` 등의 프로그램은 별도 설치가 필요하다.
- **경로:** `/secure/...`, `/backups/...`, `/projects/...`는 준비한 로컬 경로로 바꾼다.
  Windows에서는 `C:\...` 같은 해당 환경의 경로를 사용하고, 공백이 있으면 인자를 따옴표로 감싼다.
  TOML의 Windows 경로는 `'C:\tools\program.exe'`처럼 literal string을 쓰거나 백슬래시를 escape한다.
- **입력 파일:** 예시의 secret·passphrase·identity 파일은 먼저 준비한다. PowerShell 5.1의
  text pipeline은 인코딩·개행을 바꿀 수 있으므로 정확한 바이트가 필요한 secret 입력은 `--file`을 사용한다.
- **요청 ID:** `UUID`는 새 변경마다 새 UUID로 바꾼다. POSIX 셸에서는 설치된 `uuidgen`을,
  PowerShell에서는 `[guid]::NewGuid().ToString()`을 사용할 수 있다. 응답이 불확실한 재시도에는
  원래 UUID와 입력을 유지한다. `TASK_ID`, `RUN_ID`, `EXECUTION_ID`, `DIGEST`는 앞선 응답에서 가져온다.
- **출력:** 데이터 명령은 JSON을 반환한다. `ok: true`와 진단의 `ready: true`는 별개다.
  `run`은 자식 프로그램의 출력과 종료 코드를 그대로 전달한다.

## 설치와 플랫폼

- [설치와 업데이트](install.md): Homebrew·Unix 설치기, 버전·경로 선택, 후보 설치와 배포자 절차.
- [Windows 네이티브](windows.md): x64·ARM64 후보 설치, 저장 권한, PowerShell, OS 확인 창과 검증 한계.
- [WSL](wsl.md): Linux 도구·저장소, Windows와의 분리, 브라우저 DNS·CA 신뢰.
- [셸 자동완성](completion.md): zsh·bash·fish·PowerShell 설정.
- [Agent Skill](agent-skill.md): 에이전트별 설치·업데이트·제거. CLI와 Skill은 별도로 설치한다.

## 프로젝트에서 사용하기

- [값과 명령 실행](cli-contract.md): profile 연결, env override, 변수·secret, dotenv import, 이름 명령.
- [환경 진단](doctor.md): `doctor`·`diagnostics`, 도구·필수 키 선언과 복구 안내.
- [포트](ports.md): 포트 할당·binding, 프로젝트 간 주소와 worktree별 이름.
- [프로세스](processes.md): 서버 시작·로그·재시작·종료와 여러 서버의 부분 실패 재시도.
- [준비 확인](process-readiness.md): 서버가 요청을 받을 수 있을 때까지 검사·대기.
- [로컬 네트워크와 공유](local-network.md): 도메인·HTTPS·listener·외부 공유·정리.
- [기존 proxy 안내](proxy.md): 이전 링크에서 현재 네트워크 가이드로 이동.
- [Task·workstream·dashboard](tasks.md) (English): 계획·점유·검증·인계와 브라우저 관리.
- [Profile 조회와 이동](profiles.md): 메타데이터 비교, 암호화 전송, 미리보기·교체.
- [백업과 복구](backup.md): 키 준비, 암호화 백업, preview/apply와 중단 복원.
- [저장소 정리](cleanup.md): 후보 선택, 보관함 복구와 영구 삭제.

## 자동화와 API 계약

명령별 입력·출력의 최종 확인에는 설치한 CLI의 `schema COMMAND`를 사용한다.
설계 문서의 배경 설명과 현재 공개 명령을 구분한다.

- [CLI 출력 계약](cli-output.md): 출력 종류, JSON 경로, 오류, protocol 마이그레이션.
- [Task API](task-api-contract.md), [계획 편집 API](task-edit-api.md): 상태 전이·리비전·재시도.
- [Dashboard 관리 API](management-api-contract.md): 인증·Origin·변경·preview 계약.
- [포트 계약](port-design.md), [네트워크 계약](network-design.md) (English): 식별·선언·주소·소유권.
- [기존 HTTP proxy 계약](proxy-design.md): HTTP 호환성 기준. 확장 동작은 네트워크 계약을 따른다.

## 개발과 검증

- [이번 문서 개선 검토](documentation-review.md): 범위·문서별 리뷰·실행한 예제와 미검증 항목.
- [v0.25.0 배포 준비](releases/v0.25.0.md): 변경 요약, 게시 전후 절차와 복구 조건.
- [개발 안내](development.md): checkout 준비, 검사 선택, dashboard 수정과 릴리스 절차.
- [설치된 배포물 검증](../verify/README.md): Unix 시나리오와 Windows native Actions의 역할.
- [네트워크 검증 범위](../verify/network-coverage.md): 수용 기준별 검사 위치와 실제 환경의 미검증 항목.
- [계획 편집 회귀 검증](task-edit-verification.md): E01–E49 검사와 과거 검증 기준.
- [성능 측정](../perf/README.md) (English): Linux/WSL end-to-end 측정과 baseline 비교.
- [Agent Skill 원본](../skills/devtools/SKILL.md): 에이전트가 읽는 실행 지침. 사용자 설치는 위 Agent Skill 안내를 따른다.

## 설계·계획·검증 기록

아래 문서는 당시의 결정과 구현·검증 이력을 보존한다. 현재 설치 절차나 출시 상태는
위 가이드를 먼저 확인한다. 각 기록에 명시된 상태·소스 커밋·후속 문서가 적용 범위다.

- [작업 관리 설계](task-workstream-design.md), [관리 기능 로드맵](management-roadmap.md).
- [Proxy MVP 계획](proxy-plan.md), [Profile·프로젝트 lifecycle 계획](profile-project-lifecycle-plan.md).
- [안정성 개선 계획](devtools-hardening-plan.md), [저장·복구 정확성 계획](correctness-recovery-storage-plan.md),
  [프로세스·cleanup 복구 계획](process-lifecycle-recovery-plan.md).
- [Profile retirement 구현 전 계약](profile-retirement-contract.md): 제안 상태이며 설치된 기능 목록이 아니다.
- [Agent Skill 설치 계획](agent-skill-install-plan.md), [Pages 설치기 계획](pages-installer-plan.md).
- [Windows 지원 계획](windows-support-plan.md), [사전 조사](windows-support/research.md),
  [검증 정책 변경](windows-support/implementation/validation-policy.md),
  [최종 리뷰](windows-support/implementation/W9.md), [완료 기록](windows-support/implementation/completion.json).
  단계별 구현 기록과 원시 evidence는 최종 리뷰의 링크를 따라 확인한다.
