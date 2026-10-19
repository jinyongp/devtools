# N03 실제 Linux Secret Service 수용 준비

현재 결과(2026-10-19): 85e72de의 실제 GNOME 가용/부재·잠금/해제·종료, 다른 UID, PAM/logind logout 및 native CLI 복구·정리가 pass했다. 전체 범위와 한계는 [N03-final-review.md](N03-final-review.md)를 따른다. 아래는 준비·이전 실패의 역사 기록이다.

N03은 진행 중이다. 현재 WSL에서 실제 private D-Bus의 서비스 부재 검사를 실행했다.
이 호스트에는 gnome-keyring-daemon이 없어 실제 GNOME 정상·잠금·종료 검증 결과는 없다.
이를 adapter fixture나 cross compile로 통과 처리하지 않는다.

## 구현·검토 범위

기존 release workflow에 별도 disposable `vault-linux` job을 추가했다. 테스트 환경에서만
GNOME Keyring과 D-Bus를 설치하고 별도 bus·HOME·XDG 디렉터리를 만든다. bus를 시작하기
전부터 activation 환경도 임시 HOME을 가리키도록 해, 잘못된 auto-activation 시도까지
사용자의 keyring으로 향하지 않도록 했다. 제품은 계속 기존 서비스만 사용하고 새
keyring을 설치·시작하거나 passphrase backend로 fallback하지 않는다.

실제 daemon은 Python이 `--foreground`로 만든 직접 자식이며 stdin으로 합성 password를
제공한다. [배포된 GNOME manual](https://manpages.debian.org/unstable/gnome-keyring/gnome-keyring-daemon.1.en.html)의
`--unlock` 동작을 사용한다. 프로세스 handle로 그 자식만 종료·wait하고 임의 PID 탐색을
하지 않는다. private bus는 dbus-run-session 수명 안에서 종료하며 임시 HOME은 제거한다.

Go 검사는 실제 GNOME을 대상으로 기존 roundtrip/합성 item 삭제와 다음 경계를 검사한다.

- 실제 default collection을 Lock한 뒤 Locked property를 확인하고 silent Unwrap의
  `backend_locked` 및 plaintext 없음, 명시 unlock 뒤 원래 키 일치를 확인한다.
- 서비스 없음/종료 뒤에는 올바른 형식의 locator를 넘겨도 `unavailable`이어야 하며
  NameHasOwner가 계속 false여야 한다. 새 backend나 keyring을 시작하지 않는다.

collection을 잠그는 검사는 disposable host flag·native opt-in·임시 HOME 이름을 함께
요구한다. 이 검사는 실제 logout이나 WSL distro restart의 대체가 아니다. CI의 각 단계는
JSON test event에서 필요한 test의 실제 pass를 확인한다. skip/빈 match를 성공으로
삼지 않도록 같은 확인을 macOS native fixture에도 추가했다. 두 native job을 package의
필수 dependency로 유지한다.

## 실제 확인과 남은 조건

- WSL의 실제 dbus-run-session에서 서비스 부재·no activation 검사 pass(0.006s).
  raw JSON은 `/tmp/devtools-values-plan/N03-real-private-bus-absence.jsonl`에 보존했다.
- 영향 adapter/address fixture race 검사 pass(1.067s). 이는 실제 GNOME 정상/잠금 결과가 아니다.
- Python syntax와 기존 lockfile의 YAML parser로 workflow·package 의존성을 확인한다.
- source/consumer/정리·실패 경로를 재검토했고 현재 준비 범위에 확인된 적격 결함은 없다.
  실제 GNOME CI 결과, OS logout·WSL restart 및 N04/N05/A12는 계속 pending이다.

Actions dispatch나 push는 아직 수행하지 않았다. 현재 revision의 native 결과가 확보된 뒤
실패를 수정·재검토하고 N03 완료 조건을 따로 판단한다.

## 실제 GNOME 검증 결과

[1c0b22c의 vault-linux job](https://github.com/jinyongp/devtools/actions/runs/39234515925/job/118001457543)이
pass했다. artifacts의 absent/native/stopped JSONL을 대조해 각각 필수 test의 실제 pass와
skip/fail 없음, 동일 job의 정상 완료를 확인했다. [원래 test events](evidence/N03-secret-service-1c0b22c.json)를
보존한다. 초기 다운로드 stderr·GNOME MIME·기존 daemon unlock의 세 실패와 수정은
[native-ci-review.md](native-ci-review.md)에 남긴다.

이 결과는 실제 GNOME 저장/재조회·잠금/명시 해제·서비스 종료의 근거다. OS logout과
다른 사용자 peer의 검증을 대신하지 않으며 전용 root/private bus/HOME 밖의 기존
사용자 keyring을 설치·시작·잠그지 않았다. 추가 UID 검사는 별도 native 후속 작업이다.
