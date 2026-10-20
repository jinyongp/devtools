# 문서 전수 점검 — 2026-10-20

[문서 목록](README.md) · [이전 점검](documentation-review.md) · [다음 릴리스 준비](releases/v0.28.0.md)

기준 소스는 `8763df2fe3a7e607ad8c676331c577c0d2bc9c3f`다. 저장소의 Markdown
148개를 목록화하고, 현행 가이드·계약·개발 안내 35개와 Agent Skill 6개를 중심으로
본문·사용 흐름·공개 명령을 대조했다. 설계·계획 12개와 단계별 기록 95개는
당시 기준·현행 안내와의 구분·참조 경로를 점검했다. 제품 코드·workflow는 변경하지 않았다.

`index`의 writing/review 경로에서 `dev-docs`, `readme`, `review-loop`를 선택하고
문장 검토에 `korean-writing`·`english-writing`, 저장소 명령에 `devtools` 지침을 적용했다.

## 영역별 수정과 재검토

| 영역 | 확인과 수정 | 상태 |
| --- | --- | --- |
| 한·영 README·설치 | 첫 실행 명령·성공 결과 일치. 출력 그대로 전달이라는 설명을 JSON 비포장과 보호 정책으로 구분하고 업데이트와 저장 전환을 분리 | 재검토 완료 |
| 문서 색인·버전 | 최신 stable과 개발 소스 혼용을 수정. protocol v7만으로 vault 지원을 판정하지 않도록 schema 확인을 안내 | 재검토 완료 |
| 값·vault·profile·백업 | 모든 profile에 적용하는 초기화, 기존 버전 백업→writer 업데이트→명시 전환→확인 순서 추가. 전송 암호/로컬 키와 대상 vault 조건, 정확한 복구 오류 이름 보완 | 재검토 완료 |
| 선택·binding·프로세스·출력 | bundle revision 조회 예제와 주입 활성화 조건 보완. 현재 selection/output/values_update 필드, passthrough와 마스킹의 차이, 기본 notify·명시 restart·기존 앱 유지 한계를 대조 | 재검토 완료 |
| task·dashboard·API | 값 조회의 bundle/ValueID, bundle 변경 action, process bundle override·관찰 맵·저장 로그 계약 보완. task 설계의 제안/현행 경계·Windows 데이터 경로 수정 | 재검토 완료 |
| 플랫폼·네트워크 | 실제 WSL2 vault 재시작과 미검증 Windows 브라우저 네트워크를 구분. 포트 registry v4·bundle 내 var binding 조건을 연결 | 재검토 완료 |
| 개발·검증·성능·Skill | 설치물과 native keyring 검사의 차이, 12방향 bundle 전송, 최신 근거 연결을 보강. 성능의 Linux/WSL·warm/cold·관찰 한계를 확인 | 재검토 완료 |
| 릴리스 | 공개 v0.27.0과 baseline을 확인. v0.25.0 준비서를 과거 기록으로 표시하고 var/sec의 v0.28.0 준비서·전환 주의·정확한 native SHA·게시 확인 절차 추가 | 재검토 완료 |
| 과거 계획·구현 기록 | 이전 실패·미완료 표현과 원시 evidence 보존. 오래된 계획에 현행 가이드 링크 추가. var/sec 72개 보고서를 최종 판정과 단계별 이력으로 색인화 | 재검토 완료 |

## 현재 소스와 대조한 경계

- `internal/cli/commands.go`, `bundles.go`, `value_usage.go`, `vault.go`의 공개 명령과 응답,
  `internal/execution/command.go`·`internal/cli/run.go`의 선택·주입 규칙을 확인했다.
- `internal/dashboard/server.go`·`manage.go`, `internal/values/manage.go`의 조회·변경·로그
  경로를 대조했다. 현재 코드의 `vault_recovery_required` 오류와 `recovery_required` 상태를 구분했다.
- `scripts/release.mjs`, Release workflow와 공개 릴리스 메타데이터를 대조했다.
  v0.27.0은 `89100eb98888895a450b2ebd403e4e1c9b5f52d2`이며 Windows 두 ZIP과 설치기를 포함한다.
  이 버전의 설치물에 vault 기능이 있다고 설명하지 않는다.
- 기존 [Z01](var-sec/Z01-final-review.md)·[A12](var-sec/A12-final-review.md)·[B03](var-sec/B03-final-review.md)·
  [C03](var-sec/C03-final-review.md)의 source별 근거와 보장 밖 항목을 유지했다.

## 실제 수행한 검증

- 공개 v0.27.0 Linux amd64 archive와 SHA-256을 내려받아 checksum을 확인했다.
  현재 소스는 Go 1.27.2 Linux/amd64로 별도 임시 binary를 빌드했다.
- 한·영 README의 첫 실행 블록을 추출해 공개 binary와 현재 binary 각각에서 실행했다.
  네 경우 모두 env override의 `data.value=debug`와 자식 stdout `debug`를 확인했다.
- 실제 v0.27.0으로 합성 secret과 암호화 backup을 만들고 현재 binary로 passphrase vault
  전환→bundle 저장→이름 명령 실행→`[REDACTED]`→수동 lock→passive pending→unlock을 확인했다.
  전환 전 backup은 복호화 가능한 상태이고 입력 파일은 유지됐다. 모든 데이터와 키 세션은
  소유한 임시 root에만 만들고 인증된 retire 경로로 정리했다.
- `test:installed vault profiles output_redaction value_updates`의 네 설치물 시나리오가 통과했다.
  native keyring이나 실제 사용자 저장소를 변경하지 않았다.
- 양쪽 README에 기존 bootstrap 시나리오를 적용해 설치·업데이트 스트림의 성공·실패·부분 다운로드를 확인했다.
- `check:skill-release`의 공식 Skill 검증과 패키징 검사 8개가 통과했다.
- 전체 Markdown의 로컬 파일 링크와 앵커, 현행 문서의 fenced CLI 명령·옵션,
  JSON/TOML 예제 문법 및 `git diff --check`를 확인했다. 최종 수치는 아래에 기록한다.

| 최종 정적 검사 | 결과 |
| --- | --- |
| Markdown 파일 | 기존 148개 + 새 안내·색인·검토 기록 3개 = 151개 |
| 로컬 파일 링크·앵커 | 565개, 실패 0 |
| 현행 문서의 fenced CLI 호출·옵션 | 296개 호출·102개 canonical command, 불일치 0 |
| JSON·TOML 예제 | 각각 14개·26개, 문법 오류 0 |

명령 검사는 현재 binary의 `schema --all`에서 얻은 163개 canonical command와 alias를
기준으로 이름·옵션을 대조했다. 임의 프로젝트 도구·placeholder의 실행 가능성이나
모든 예제의 실제 성공을 보증하는 검사는 아니며, 직접 실행한 흐름은 위 항목과 같다.

## 확인된 배포 도구 문제

원본 `node scripts/release.mjs`의 비게시 계획 실행은 `spawnSync gh ENOBUFS`로 실패했다.
GitHub releases 응답이 1,073,852 bytes로 기본 child-process buffer 1,048,576 bytes를 넘는다.
같은 공개 릴리스·후보에 포함된 tag·커밋 분류 규칙을 별도로 적용하면 이전 버전은
`v0.27.0`, 다음 minor 권장은 `0.28.0`이다. 수동 확인과 명시 workflow 입력은
[개발 안내](development.md#릴리스)·[배포 준비서](releases/v0.28.0.md)에 정리했다.
기존 script의 이 오류를 수정하거나 실패한 실행을 통과로 기록하지 않았다.

## 최종 리뷰 범위와 한계

수정한 문서를 다시 읽고 변경이 연결된 README·색인·설정·이동·API·릴리스 안내를
함께 재검토했다. 역사 문서의 본문 전체를 현재 구현 계약으로 재작성하거나 모든 과거
검증 결과를 새로 실행하지 않았다. 각 기록은 그 파일의 소스·날짜에만 적용된다.

문서 점검에서 OS별 native·전체 Go suite·브라우저를 다시 실행하지 않았다. 해당 기존 근거를
이번 문서가 입력이었던 실행처럼 표시하지 않는다. PowerShell/macOS 예제 전체, 외부 링크
전체의 접속, Homebrew·외부 agent 설치·실제 OS 인증 창은 이번에 새로 실행하지 않았다.
릴리스 게시와 실제 사용자 자료의 전환도 수행하지 않았다.

현행 문서 검토에서 남은 수정 대상은 없다. 배포 도구의 ENOBUFS는 위의 별도 구현 문제로 남는다.
