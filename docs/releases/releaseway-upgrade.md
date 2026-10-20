# Releaseway 연동 업데이트 — 2026-10-20

기존 작업 `319d4a3`까지 로컬 main에 fast-forward 병합했다. 제품 소스와 기존 검증 SHA를
보존하고 아래 릴리스 도구 변경을 main에서 진행했다. 실제 릴리스·tap·Pages 게시는 실행하지 않았다.

**게시 전 해결할 문제:** 공개 `releaseway/actions v0.6.0`도 이 저장소의 릴리스 목록을
읽다가 `spawnSync gh ENOBUFS`로 실패한다. 아래 소비자 업데이트만으로 실제 게시가
준비됐다고 판단하지 않는다. 업스트림 수정이 포함된 공개 버전으로 pin을 바꾸고
미리보기를 다시 통과해야 한다.

## 반영한 변경

- [Actions v0.6.0](https://github.com/releaseway/actions/releases/tag/v0.6.0)의
  `804562f2eb9d7662810298835f7339bd6f26fcfa`로 게시 액션을 갱신했다.
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

## 발견한 업스트림 문제와 준비한 패치

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

## 검증과 한계

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

업스트림의 전체 연동 예제에는 미공개 npm 입력과 교체할 placeholder SHA도 있다.
devtools에는 npm 게시가 없으며 Homebrew의 기존 commit/version 연동은 호환된다.
Pages reusable workflow로 전체를 옮기는 대신 현재 배포 단계에 검증 step만 적용했다.
