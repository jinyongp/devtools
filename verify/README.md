# 설치된 release 검증

devtools의 설치 후 공개 CLI 흐름을 실제 패키징된 실행 파일로 검증한다. Docker는 필요하지 않다.
runner는 테스트용 release를 만들고, 각 시나리오를 독립적인 HOME·XDG 경로·작업 디렉터리에서 실행한다.

GitHub Actions에서는 fresh hosted runner가 OS 환경 격리를 담당한다. 로컬 실행도 사용자 데이터와
Git 전역 설정을 별도 임시 경로로 분리하므로 현재 devtools 상태를 건드리지 않는다.

## 실행

```sh
# 전체 시나리오
just verify

# 설치·업데이트 시나리오
just verify install

# 작업 관리·인계·대시보드 시나리오
just verify tasks

# 환경 진단·명령 실행 전 필수 조건 시나리오
just verify doctor

# 포트 할당·실제 서버 실행·프로젝트 간 참조
just verify ports

# reverse proxy route·WebSocket·daemon 재시작
just verify proxies

# 암호화 백업·복구·재시도·충돌·직전 백업
just verify backup

# Profile 탐색·값 비노출·recipient·import preview/apply와 동시 replay
just verify profiles

# 실제 서버의 다중 시작·readiness·재사용·worktree 격리·부분 재시도
just verify project_lifecycle

# 프로세스 준비 확인·대기·취소·실행 기준 유지
just verify readiness

# 시나리오 목록
just verify --list
```

기본 실행은 현재 OS/architecture용 `0.0.0-test.1`, `0.0.0-test.2` CLI release와
`0.0.0-test.1` Agent Skill artifact를 임시 디렉터리에 만든다. 모든 시나리오가 끝나면
release와 사용자 데이터가 자동으로 삭제된다. 시나리오가 실패해도 runner가 격리 HOME에서
시작한 proxy, dashboard, managed process를 정리한 뒤 임시 데이터를 삭제한다.

Go, Python 3, POSIX sh, tar, Git, curl과 OpenSSL이 필요하다. completion 시나리오는 설치된
bash·zsh·fish를 감지해 사용할 수 있는 shell만 검증한다. Release CI의 Linux runner는
zsh와 fish를 명시적으로 설치해 세 shell을 모두 확인한다.

각 시나리오는 임시 HOME의 `.local/bin/devtools`에 release를 설치한다. macOS 데이터는
해당 HOME 아래 `Library/Application Support/devtools`, Linux 데이터는 격리된 XDG 경로에
저장한다. `GIT_CONFIG_GLOBAL=/dev/null`과 `GIT_CONFIG_NOSYSTEM=1`도 적용한다.

## 기존 release 검증

이미 만든 테스트 release를 사용할 때는 runner를 직접 실행한다.

```sh
python3 verify/run.py --releases /path/to/releases all
```

`--releases` 디렉터리에는 선택한 호스트용 CLI archive와 checksum이 있어야 한다.
`discovery` 시나리오를 실행한다면 Agent Skill archive와 checksum도 포함한다.

## 검증 경계

Go unit test는 port 가용성이나 실제 listener가 테스트 목적이 아닌 경우 probe/listener를
주입해 결정적으로 실행한다. 실제 TCP bind, occupied/free 판정, IPv4/IPv6 listener,
설치·업데이트, detached process, proxy HTTP/WebSocket 같은 OS 동작은 이 시나리오에서
패키징된 실행 파일로 검증한다.

GitHub-hosted runner는 job마다 새 VM을 사용하므로 release 검증의 공식 clean OS 환경이다.
Docker의 `--network none`, capability 제거 같은 별도 sandbox 제약은 더 이상 release
gate에 포함하지 않는다. 외부 네트워크가 필요한 시나리오는 없으며, 설치 시나리오의 HTTPS
검증도 로컬 테스트 서버를 사용한다.

## 시나리오 추가

`verify/scenarios/<이름>.py`를 추가하면 runner가 파일명으로 시나리오를 찾는다.
`all`은 등록된 전체 시나리오를 이름순으로 실행한다. 여러 이름을 전달하면 지정한 순서로
실행한다.

시나리오는 제공된 HOME 안에서 필요한 프로젝트·파일·설치 상태를 준비하고 공개 CLI를
사용한다. 앞선 시나리오의 상태에 의존하지 않아야 한다. 성공 시 종료 코드 0, 실패 시
0 이외의 값을 반환한다.

`install`은 설치·업데이트, profile 값, 혼합 dotenv 가져오기, 프로젝트 명령, worktree,
파일 권한, HTTPS 다운로드, 신호 전달을 검증한다. `proxies`는 두 worktree의 route,
404·502·503 응답, WebSocket, 동적 port 변경, daemon 재시작과 listener reservation을
검증한다. `tasks`는 명세·계획·검증 등록, 동시 점유, worktree 공유, 체크포인트·인계·완료
재시도, dashboard의 일회용 링크·세션·종료를 검증한다.
