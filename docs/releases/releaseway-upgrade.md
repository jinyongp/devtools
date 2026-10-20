# Releaseway 연동 업데이트 — 2026-10-20

## 현재 적용: Actions v0.6.3 / Homebrew Actions v0.4.3

공개 안정 릴리스와 annotated tag가 가리키는 커밋을 확인해 다음 SHA로 갱신했다.
업스트림 README의 일부 예제에는 이전 버전이 남아 있어 공개 Release·태그·실제 소스를
기준으로 선택했다. 프로젝트 패키지 의존성이나 lockfile 변경은 없다.

- [Actions v0.6.3](https://github.com/releaseway/actions/releases/tag/v0.6.3):
  `2abdf26c6f01c1e49da4ba5d85253847fe721fd9`.
  게시 액션과 설치기 준비용 `distribute` 액션을 함께 갱신했다.
- [Homebrew Actions v0.4.3](https://github.com/releaseway/homebrew-actions/releases/tag/v0.4.3):
  `8fe42d36a7f93a3c399461efbe571fc695d4cdc6`.
  check/publish reusable workflow를 함께 갱신했다.

### 소비자에 적용되는 변경

릴리스 노트 준비는 checkout의 조건부 include 인증을 임시 Git 저장소에 전달하고,
성공·실패 모두 임시 증거 저장소를 정리한다. 긴 first-parent 이력과 merge graph는
상한이 있는 묶음으로 읽는다. 기존 `notes: standard`, 원본 태그·commit 검증,
`diagnostics-path` 등 입력·출력은 호환되므로 호출 계약은 유지했다.

Homebrew는 생성한 Formula의 SHA-256을 native 검증부터 commit·rebase·push까지 전달한다.
작업 파일과 `HEAD`의 Formula blob이 모두 그 바이트와 같아야 게시한다. 동일 Formula의
동시 변경이 Git에서 충돌 없이 합쳐져도 중단할 수 있다. 이 경우 push job만 재시도하지 않고,
현재 tap에서 Formula를 다시 생성하고 native 검증부터 게시까지 전체 단계를 재실행한다.
`allow-downgrade`는 이 바이트 검증을 우회하지 않는다. 현재 소스 빌드 Formula와
macOS ARM64 게시 검증 범위는 유지하며 Linux·Intel 설치 검증으로 해석하지 않는다.

### 조사 후 유지한 경계

큰 이전 Pages 응답을 재시도하고 스트림을 취소하는 수정은 업스트림의 reusable Pages
workflow에 적용된다. devtools는 `distribute`로 준비한 설치기를 태그 소스와 byte 비교한 뒤
별도 Pages 단계로 게시하므로 그 served-verification 경로를 사용하지 않는다.
내부 action을 직접 호출하거나 전체 Pages 흐름을 교체하지 않았다.

양쪽 저장소의 `distribution-run` 증거 강화는 Releaseway 자체 릴리스용 workflow 변경이다.
devtools가 호출하는 공개 게시 액션·Homebrew workflow에 새 필수 입력을 추가하지 않는다.

### 검증

[읽기 전용 검증 기록](evidence/releaseway-v0.6.3-readonly.json)에 공개 bundle의 SHA-256과
실제 `v0.29.0` 노트 미리보기·설치기 검증 결과를 보존했다.
Node 24.22.0에서 수정하지 않은 공개 bundle로 `state=preview`, 진단 성공,
임시 증거 저장소 정리와 설치기 두 파일의 태그 소스 byte 일치를 확인했다.
업스트림 Actions 테스트 159개와 공개 bundle 재빌드 일치, Homebrew 게시 script·workflow
계약 검사, devtools 릴리스 테스트 10개·Windows 검증 재사용 테스트 7개,
변경한 네 workflow의 actionlint v1.7.12를 통과했다.

`.github/workflows/release-tooling.yml`은 main의 관련 변경과 PR·수동 실행에서 같은
미리보기·설치기 준비·소스 비교를 `contents: read` 권한으로 검증한다.
최신 공개 릴리스 이름만 GitHub CLI로 조회하고, 안정 버전·태그의 원격 바인딩과
main ancestry 검증은 공개 `prepare` 액션의 `mode: resolve`에 맡긴다.
현재 릴리스의 두 설치기를 검사하며 과거 Unix-only 태그용 분기는 넣지 않았다.
임시 Git 저장소의 내부 정리 방식은 소비자 CI에서 검사하지 않고 업스트림 테스트에 맡긴다.
게시 액션이 `publish=false`에서 생략되는 기존 Release 검증의 빈 부분을 보완한다.
Homebrew 사전 검사도 관련 main push에서 실행한다. 새 제품 릴리스·tap·Pages 게시는
이번 업데이트의 검증에 포함하지 않는다.

## 이전 v0.6.1 업데이트 기록

이하 내용은 당시 조사·검증 기록이다. 이후 v0.29.0의 실제 게시·Homebrew·Pages 배포는
[Release 실행](https://github.com/jinyongp/devtools/actions/runs/39428641454)에서 성공했다.

기존 작업 `319d4a3`까지 로컬 main에 fast-forward 병합했다. 제품 소스와 기존 검증 SHA를
보존하고 아래 릴리스 도구 변경을 main에서 진행했다. 실제 릴리스·tap·Pages 게시는 실행하지 않았다.

**업스트림 오류 해결 확인:** 공개 `releaseway/actions v0.6.1`로 두 pin을 갱신했다.
수정하지 않은 공개 bundle로 기존 실패 조건의 릴리스 노트 미리보기를 다시 실행해
`state=preview`, 진단 `status=success`를 확인했다. 기존 `spawnSync gh ENOBUFS`는
재현되지 않았다. 실제 게시·Homebrew 갱신·Pages 배포는 아직 실행하지 않았다.

## 반영한 변경

- [Actions v0.6.1](https://github.com/releaseway/actions/releases/tag/v0.6.1)의
  `1b1d72999b59f03d334845f5accb982c38f4afe1`로 게시 액션을 갱신했다.
  새 `diagnostics-path` 출력은 성공·실패 후 `release-diagnostics` artifact로 보존한다.
  진단 업로드 실패는 게시 성공과 후속 Homebrew·Pages 실행을 막지 않는다.
  진단 파일이 없는 runner/filesystem 실패까지 기록을 보장하지는 않는다.
- [Homebrew Actions v0.4.2](https://github.com/releaseway/homebrew-actions/releases/tag/v0.4.2)의
  `c1be213c09e18836142bbad3561a0feda507536a`로 check/publish 호출을 함께 갱신했다.
  기존 `commit`·`version`·tap deploy key 계약은 유지한다. 이 버전의 asset Formula 버전
  보존 수정은 포함되지만 현재 devtools Formula는 소스를 Go로 빌드한다.
- Pages는 같은 Actions SHA의 `distribute` step으로 공개 Release의 installer를 준비한다.
  checkout한 태그의 commit SHA, asset digest·크기, 재조회한 Release snapshot을 검증한다.
  기존 소스와의 byte 비교, shell 구문 검사, 배포 경로와 Pages 권한을 유지한다.
  과거 Unix-only 태그에는 `install.ps1`을 요구하지 않는다.
  `installer-distribution` artifact에 선택한 두 installer의 검증 보고서를 보존한다.
- `release:plan`/`release:publish`의 자체 버전 조회는 `gh --jq`에서 공개 안정 태그 이름만
  추려 Node.js에 전달한다. 본문·자산 metadata로 subprocess 버퍼를 채우지 않으며
  실패·미게시 태그 제외와 기존 0.x breaking 변경의 minor 증가 정책을 유지한다.

## v0.6.1 재검증

업스트림은 릴리스 목록과 PR 목록을 `gh --jq`에서 필요한 필드로 줄이고,
릴리스 본문 조회도 불필요한 자산 metadata를 제외하도록 수정했다. JSON으로 이스케이프한
본문을 수용하도록 CLI 출력 한도는 8MiB로 명시했다. 페이지 수와 잘못된 응답 검증은 유지한다.

[v0.6.1 검증 기록](evidence/releaseway-v0.6.1-readonly.json)에 공개 bundle의 SHA-256과
실제 미리보기·installer 검증 결과를 보존했다. Node 24.22.0에서 확인했다.

- 기존 실패와 같은 `v0.28.0`, 원본 `3ca717a`, `notes: standard`로 실제 미리보기 성공.
  `notes-state=prepared`, 원격 태그 일치와 진단 `operation=preview`, `status=success` 확인.
- 같은 공개 Release의 installer 두 개를 새 배포 액션으로 준비해 commit, digest·크기,
  저장소 script와의 byte 일치 확인.
- 업스트림 GitHub adapter 회귀·계약·액션·진단·배포 테스트 49개 통과.
  큰 응답, 페이지 경계, 잘못된 응답, 출력 한도, UTF-8 본문 보존 검사를 포함한다.
- devtools 릴리스 테스트 10개와 변경한 두 workflow의 actionlint v1.7.12 통과.
  별도 shellcheck·pyflakes는 실행하지 않았다.

v0.6.1 재검증 당시 Homebrew Actions 최신 버전은 기존 pin의 v0.4.2여서 변경하지 않았다.
아래 v0.6.0 제안 패치는 당시 조사 기록이며 현재 workflow에는 적용하지 않는다.

## v0.6.0에서 발견한 업스트림 문제와 제안 패치

[업스트림 GitHub API 구현](https://github.com/releaseway/actions/blob/804562f2eb9d7662810298835f7339bd6f26fcfa/src/github.ts)은
`spawnSync`의 기본 1MiB 버퍼로 릴리스 목록 전체를 받는다. 실제 첫 페이지의 43개
릴리스 응답은 1,104,465 bytes였다. 목록에서 사용하는 필드는 `tag_name`, `draft`,
`prerelease`뿐이다. 같은 버퍼 제한은 이전 v0.4.1 코드에도 있다.

[3줄 수정 패치](evidence/releaseway-v0.6.0-release-list.patch)는 이 목록 호출에만
`gh --jq` projection을 추가한다. 배열 길이·페이지 수·선택 필드를 유지하며 다른 API나
subprocess의 허용 크기는 바꾸지 않는다. v0.6.0 원본에 `git apply --check`를 통과했다.
임시 업스트림 복사본에만 적용해 재빌드했으며 원격 Releaseway 저장소나 공개 버전은 변경하지 않았다.

수정하지 않은 공개 bundle의 실제 `notes-preview`는 위 오류로 실패했다.
패치를 적용한 bundle은 같은 `v0.28.0`·원본 SHA·`notes: standard`로
`state=preview`, 진단 `status=success`를 반환했다. 게시 동작은 수행하지 않았다.

## 이전 v0.6.0 검증과 공통 한계

[읽기 전용 근거](evidence/releaseway-v0.6.0-readonly.json)에 버전 SHA, installer digest와
업스트림 오류·수정 검증을 기록했다.

- devtools 릴리스 테스트: Node 22와 Node 24에서 각각 10개 통과.
  큰 릴리스 응답 회귀, 안정 버전 선택, 태그 충돌·재시도·main 외 게시 거부를 포함한다.
- 실제 `release:plan`: `ENOBUFS` 없이 `v0.28.0 → v0.28.1 (patch)` 계산 성공.
- 변경한 세 workflow: actionlint v1.7.12 통과. 별도 shellcheck·pyflakes는 실행하지 않았다.
- 수정하지 않은 업스트림의 진단·배포 테스트 22개와 계약·액션 테스트 18개 통과.
- 공개 `v0.28.0`의 installer 두 개: 배포 액션으로 읽기 전용 준비 후 태그 commit,
  GitHub asset digest·크기·원본 script byte 일치 확인.
- 제안 패치: 관련 업스트림 테스트 32개와 실제 공개 릴리스 노트 미리보기 통과.

기존 [D02의 native OS·패키징 검증](../var-sec/D02-release-validation.md)은 고정한
제품 소스에만 적용한다. 이 업데이트는 제품 Go 코드를 바꾸지 않지만 기존 비게시 실행에서
skip했던 게시용 액션의 성공을 대신하지 않는다. 실제 새 Release 게시, Homebrew 갱신,
Pages 배포와 게시 후 설치 확인은 수행하지 않았다.

## 별도 검토가 필요한 변경

v0.6.0의 `prepare`는 `strategy: source-tag`와 plan/resume/tag를 제공한다.
이 프로젝트도 이미 원본 커밋에 태그를 붙이지만 전체 planner로 교체하려면
현재의 실제 게시된 안정 버전 기준과 0.x breaking 변경 정책을 보존해야 한다.
따라서 이번에는 버전 계산·태그 생성 전체를 교체하지 않았다.

v0.6.1의 전체 연동 예제는 공개된 Actions v0.6.0·npm Actions v0.7.0·Homebrew v0.4.2의
고정 SHA를 사용하도록 갱신됐다. devtools에는 npm 게시가 없으며 Homebrew의 기존
commit/version 연동은 호환된다.
Pages reusable workflow로 전체를 옮기는 대신 현재 배포 단계에 검증 step만 적용했다.
