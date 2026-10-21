# 실행 출력 보호

[문서 목록](README.md) · [값과 명령](cli-contract.md) · [로컬 secret 보호](secret-protection.md)

출력 보호는 v0.28.0부터 제공한다. 설치한 빌드에서
`devtools schema command run`으로 `output`, `stdout`, `stderr` 옵션을 확인한다.
기본 `auto`는 새 실행의 non-TTY stdout·stderr를 각각 마스킹하고, 실제 TTY는 raw로
전달한다. Agent도 PTY를 사용할 수 있으므로 TTY 여부를 사람의 신원으로 해석하지 않는다.

## 모드 선택

명령 설정에는 전체 모드와 stream별 예외를 지정할 수 있다.

```toml
[commands.api]
exec = ["pnpm", "dev"]
inject = true
output = "auto"

[commands.archive]
exec = ["my-archive-tool"]
inject = true
stdout = "raw"
stderr = "redacted"
```

`my-archive-tool`은 바이너리 출력 도구를 나타내는 예시다. 실제 설치된 도구로 바꾼다.
전체 모드는 `auto`, `redacted`, `raw`이며 stream별 설정에도 같은 값을 쓴다.
`redacted`는 TTY에도 필터를 적용하고, `raw`는 명시적인 보호 예외다.
바이너리를 자동 감지해 raw로 전환하지 않는다.

CLI 옵션은 설정한 명령 이름이나 직접 실행의 `--`보다 앞에 둔다.

```sh
# stdout만 raw로 내보내고 stderr는 기본 보호 유지
devtools run --stdout raw archive > output.bin

# PTY를 쓰는 도구에서도 두 stream을 보호
devtools run --output redacted api
```

등록된 `archive`, `api` 명령을 전제로 한 예다. `command inspect NAME`의 `output`은
요청 정책을 보여 준다. 새 managed 실행의 `process status EXECUTION_ID`는
`item.output`에 요청 정책, 실제 stdout/stderr 모드, 선택 이유를 기록한다.
이유는 `explicit`, `tty`, `non_tty`, `capture`다.

`--capture-logs`는 **raw 정책과 관계없이 저장 전에 마스킹**한다. stdout과 stderr의
독립 필터 결과만 기존 bounded log 저장소에 전달한다. 캡처하지 않으면 managed
출력은 기존처럼 버린다. 과거 빌드가 이미 저장한 로그를 소급해 보호한다는 뜻은 아니다.

## 일치와 수명

패턴은 실제 전달한 non-empty secret과 JSON 문자열 body의 알려진 표현이다.
JSON 표현은 Go `encoding/json`의 HTML escaping을 켠 형태와 끈 형태를 지원한다.
원문 byte 일치에서 왼쪽의 가장 긴 패턴을 먼저 `[REDACTED]`로 대체한다.
UTF-8 경계나 개행, 여러 write로 쪼개진 값도 처리한다. stdout/stderr는 상태를 공유하지 않는다.

빈 값은 패턴을 만들지 않는다. 1~3 byte 값도 제외하지 않으며, 해당 실행은 공개
`short_secret_patterns`로 로그가 많이 가려질 수 있음을 알린다. 값 길이 자체는 공개하지 않는다.
Vault 잠금이나 저장 값 변경은 이미 실행 중인 앱의 옛 패턴을 지우지 않는다.
해당 execution의 출력이 끝날 때 정리하며, 아직 처리 중인 callback이 있으면 필요한
상태를 유지한다. Go 문자열과 OS 환경이 즉시 물리적으로 지워지는 secure heap은 아니다.

앱이 만든 파일·네트워크·별도 FD, 알 수 없는 새 token, 임의 base64·압축·암호화,
서로 다른 stream으로 나눈 의도적 유출은 보호 범위 밖이다. 매칭 필터는 앱 격리를 제공하지 않는다.

## 한도와 실패

정규화·중복 제거한 패턴에 다음 한도를 적용한다.

| 항목 | 한도 |
| --- | --- |
| 패턴 수 | 4,096 |
| 단일 패턴 | 64 KiB |
| 패턴 byte 총량 | 4 MiB |
| matcher 상태 수 | 262,144 |
| 한 상태의 일치 suffix 수 | 64 |

JSON escaping으로 늘어난 표현도 한도에 포함한다. OS 환경 한도를 통과했어도 출력 보호
한도에 걸릴 수 있다. 초기화가 실패하면 `redaction_unavailable`로 실행을 거절하며,
managed restart도 기존 실행을 멈추기 전에 거절한다. 패턴을 조용히 버리거나 raw로 바꾸지 않는다.

각 stream은 최장 패턴보다 한 byte 작은 lookahead, 같은 크기의 match 길이 ring,
32 KiB 출력 block을 사용한다. 실제 비밀 prefix일 수 있는 부분만 보류하며, 관련 없는
시작 메시지는 즉시 전달한다. 비밀 prefix로 끝난 짧은 출력은 다음 입력이나 EOF까지 지연될 수 있다.
터미널·pipe·파일 출력의 느린 소비자에는 마스킹한 byte만 작은 bounded queue로 전달한다.
캡처 디스크 쓰기는 기존 저장 수명과 잠금에 맞춰 동기 처리한다. 평문 디스크 spool은 만들지 않는다.

준비 때 TTY였던 stream이 실행 전에 다른 대상으로 바뀌면 `output_stream_changed`로
새 준비를 요구한다. 보호된 출력 전달에 실패하면 자식을 취소하고 `output_protection_failed`로 보고한다.
사용자 취소나 신뢰할 수 없는 EOF에서는 보류한 prefix를 강제로 내보내지 않는다.
OS 출력 callback이 중단될 수 없는 경우 CLI 종료까지 마스킹된 block이 남을 수 있지만,
그 callback이 원문 입력이나 matcher를 소유하지 않도록 분리한다.
EOF·취소·신호·자식의 nonzero exit는 출력 보호와 구분한다.
EOF 전에 완전한 패턴과 일치하지 않은 나머지는 정의한 literal-match 규칙을 따른다.

실제 OS별 동작과 성능의 배포 판정은 native/통합 gate에서 별도로 확인한다.

## 외부 실행기에 전달한 값의 출력

[승인된 로컬 실행기](secret-automation.md)는 같은 matcher를 사용하지만 기존
`devtools run`의 TTY별 `auto` 선택과 별도 계약이다. 참조 수신기는 비공개 채널로 받은
패턴을 두 stream에 설치한 뒤 수신을 확인하고, TTY 여부와 관계없이 출력 종료까지 보호한다.
이 경로에는 raw fallback이 없다. 공개 JSON에 패턴이나 `short_secret_patterns`를
추가로 노출하지 않는다.

직접 만든 실행기의 보호 여부를 OS 사용자 인증만으로 증명할 수는 없다.
호출 시스템이 앱의 모든 로그 경로를 보호해야 하며 Docker·CI 로그를 자동으로
연결해 주지는 않는다. 전달 성공은 실행 시작이나 외부 인증 성공과 구분한다.
