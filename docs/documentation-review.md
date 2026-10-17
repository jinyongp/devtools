# 문서 개선 검토 — 2026-10-17

[문서 목록](README.md)

Windows 구현 완료 후 README와 현행 문서의 사용 흐름·플랫폼 안내를 정리한 별도 작업이다.
기준 checkout은 `ccb69ca932a507dccdaf179cff5160abf180a9fc`이며 변경은 Markdown 문서에 한정한다.
기존 Windows workstream은 완료 상태로 보존하고, 문서 작업은 독립 task
`671f412a-f310-43ae-8f1d-73dd4b667cf4`로 관리한다.

## 적용 지침과 범위

설치된 `index`의 writing 경로에서 `readme`·`dev-docs`를 선택하고 구조·검증 지침을 적용했다.
한·영 문장은 `korean-writing`·`english-writing`으로 검토하고 `review-loop`로 수정 후 재검토했다.

전체 Markdown 목록을 확인해 현행 가이드·API 계약·개발 안내와 과거 설계·검증 기록을 구분했다.
현행 문서는 공통 색인으로 연결하고, 발견한 사용 흐름·플랫폼 설명의 문제를 수정했다.
과거 계획, Windows 단계별 구현 기록, 원시 evidence와 Skill 원본은 그대로 보존했다.
역사 기록 전체를 현재 가이드로 다시 쓰거나 모든 본문을 새로 줄 단위 검토한 것은 아니다.

## 문서별 검토와 재검토

| 영역 | 발견한 문제와 수정 | 최종 상태 |
| --- | --- | --- |
| 한·영 README | 플랫폼 검증 세부사항이 소개에 섞임. 지원 환경을 분리하고 공통 첫 실행·플랫폼별 child 실행·성공 출력을 제시. 기존 profile 공유 조건을 실행 전에 안내 | 검토·재검토 완료 |
| 전체 문서 탐색 | 사용자 가이드·API·개발·역사 기록을 한곳에서 찾기 어려움. 문서 색인과 현행 문서의 돌아가기 링크 추가. 제안 상태인 retirement 계약을 현행 기능과 구분 | 완료 |
| 설치·개발 안내 | 사용자 설치보다 배포 빌드가 앞에 나옴. 설치·업데이트를 먼저 배치하고 개발 도구·빌드·검사 선택 순서 정리. Unix runner와 Windows Actions 검사 구분 | 완료 |
| 값·프로세스·준비 검사·포트 | Unix 신호·권한만 설명하거나 셸 전제가 불분명함. Windows Job·DACL·Known Folder·상위 host Job 한계, POSIX 전용 예제와 경로·UUID 규칙 명시 | 완료 |
| Windows·WSL·자동완성·전송 | 후보 파일을 구하는 경로와 stable/후보 차이를 찾기 어려움. 같은 Actions artifact의 파일 사용, WSL Linux 설치·진단, PowerShell 후보 버전, 별도 저장소·전송 경계 정리 | 완료 |
| API·검증·성능 문서 | v7 마이그레이션이 끝에 묻힘. 최신 전환을 먼저 배치하고 기존 앵커 보존. Unix system-network와 Windows native 결과를 구분하고 변동하는 명령 개수의 고정 표현 제거 | 완료 |
| 과거 계획·증거 | 소스별 과거 결과와 현재 제품 결과가 섞이지 않는지 확인. 원문·checksum·완료 영수증은 변경하지 않고 색인에서 적용 범위를 안내 | 보존 확인 완료 |

## 검증 근거

- [공개 stable v0.24.1](https://github.com/jinyongp/devtools/releases/tag/v0.24.1)의 자산 목록을 조회했다.
  macOS/Linux 네 대상이며 Windows ZIP·`install.ps1`은 없다.
- 공개 Linux amd64 archive의 SHA-256을 확인하고 해당 release의 `install.sh`로 임시 bin에 설치했다.
  `devtools`와 `dvt` 모두 `0.24.1`을 반환했다. 사용자 설치본·데이터는 변경하지 않았다.
- 양쪽 README에서 공통 첫 실행 명령을 추출해 일치 여부를 확인했다. 공개 v0.24.1과 현재 후보 모두
  `init`·공통 값·env 생성·override·`data.value=debug`·자식 stdout `debug`를 확인했다.
- 현재 소스의 Linux CLI를 빌드하고, 네 셸 completion 생성과 WSL `data.platform.runtime=wsl`을 확인했다.
  생성된 PowerShell 스크립트는 로컬에서 실행하지 않았다.
- 준비 확인 문서의 Python·TOML을 추출해 임시 프로젝트에서 실제
  `process start`→`check`→`wait`→`stop`을 수행했다. `wait`의 `readiness.ready=true`를 확인했다.
- 기존 `verify/scenarios/bootstrap.py`를 한·영 README에 각각 적용해 install/update 스트리밍·실패·부분 다운로드 fixture를 통과했다.
- 전체 Markdown의 로컬 파일 링크·앵커와 `git diff --check`를 검사했다.
  외부 웹 링크 전체의 접속 가능성을 검사한 것은 아니다.

임시 검증 도구는 OS 임시 디렉터리에만 두었다. 제품 코드·workflow·의존성과 영구 test 파일은 변경하지 않았다.

## 전체 리뷰루프와 한계

문서별 수정 후 전체 변경을 다시 읽고 한·영 의미, 설치→첫 실행→결과→상세 안내의 연결,
기존 앵커, 후보/게시본 구분, OS별 셸·저장·수명 차이, 역사 기록 보존을 재검토했다.
검토 범위 안에 남은 수정 대상은 없다.

Windows 실행 근거는 계속 소스 `50ccb27b5df2d47ad56a1b24570dde66b9e7a2ba`의
[기존 최종 Actions 검증](windows-support/implementation/W9.md)에 한정한다.
이후 문서 수정이 그 실행의 입력이었다고 주장하지 않는다. 이번 작업에서 Windows 바이너리를
로컬 실행하거나 Windows CI·전체 runtime suite를 반복하지 않았다.
Windows 예제의 shell·Job·권한 설명은 해당 구현·기존 Actions 근거와 대조했다.
실제 PC의 UAC·브라우저 신뢰·터미널 종료·설치 관찰, 최종 WSL2↔Windows 브라우저 네트워크,
지원 OS×CPU 전체 조합과 외부 provider/LAN은 계속 미검증이다.
Homebrew 설치와 외부 agent 설치도 이번 문서 검사에서 새로 실행하지 않았다.
