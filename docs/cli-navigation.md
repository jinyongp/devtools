# 목록 탐색과 복구 안내

이 문서는 protocol 23 소스의 계약이다. 설치본은 `devtools version`과 해당 명령의
`schema`로 확인한다. JSON 자동 선택 여부에 따라 목록의 범위나 기본 항목은 바뀌지 않는다.

## 현재 상태와 이력

`devtools process list`는 현재 프로젝트의 서비스를 보여 준다. 한 서비스는 profile,
실제 프로젝트 경로, command의 조합이다. 현재 실행과 마지막 시도는 별도로 표시한다.
기존 서버를 유지한 채 restart 준비만 실패하면 현재 Running과 마지막 실패를 함께
확인할 수 있다. 복수 활성 실행은 conflict와 모든 execution ID로 표시한다.

실행별 이력은 다음처럼 조회한다. `my-app`은 실제 profile 이름으로 바꾼다.

```sh
devtools process list --profile my-app --view history --limit 50
devtools process list --profile my-app --state running --state unknown
devtools process list --all-profiles
devtools --format text process list --columns command --columns current_state --columns last_attempt
```

기본 후보는 serve 선언과 관리 실행 이력이 있는 명령이다. 아직 실행하지 않은 서버는
Not started다. 정상 종료는 Exited이며, 후속 성공으로 해결된 과거 실패는 history에 있다.
제거한 명령도 살아 있거나 결과가 불명인 실행이 있으면 현재 목록에서 확인할 수 있다.
`process status ID`, stop/restart/logs의 ID는 서비스 key가 아닌 실제 execution ID다.

`expose list`와 `values execution list`도 current가 기본이고 `--view history`로
종료 이력을 본다. 전달 중이거나 결과가 불명인 값 전달을 만료로 간주해 숨기지 않는다.
task의 기존 할 일·완료·stale completion 의미는 유지한다.

## 범위와 조건

프로젝트 자원은 기본 현재 canonical directory다. `--profile`은 해당 profile의
모든 위치를 고르고, `--dir`과 `--instance`를 함께 주면 교집합이다. `--all-profiles`는
명시적인 전체 조회다. 프로젝트 밖에서 범위를 알 수 없으면 scope_required로 종료하고
profile/instance 선택 경로를 안내한다. profile 목록과 보관함은 원래 전역 자원이다.

목록은 필터, 안정 정렬, 페이지 순으로 처리한다. 반복 `--state`는 OR이고 다른 조건은
AND다. `--query`는 공개 이름·명령만 검색한다. 값·비밀·원문 로그는 검색하지 않는다.
`--since`, `--until`은 timezone이 있는 RFC3339를 받고 범위는 `[since, until)`다.
`--time-field`에 선택할 수 있는 실제 시각은 해당 명령의 schema에 있다.
없는 종료 시각을 파일 수정 시각으로 추정하지 않는다.

페이지 기본 크기는 50, 허용 범위는 1..200이다. 다음 페이지는 응답의 실제 cursor와
동일한 범위·필터·정렬을 사용한다. 응답의 ready next action은 이 조건을 보존한다.
페이지 사이에 원본이 바뀌어도 같은 snapshot을 읽는다. cursor는 30분 뒤 만료되며
조건·root를 바꾸면 cursor_invalid다. 표시 개수를 확인하지 않은 전체 개수로 해석하지 않는다.

읽기 전용 데이터와 별도 쓰기 가능한 private cache를 함께 사용할 수 있다.
`--query-cache-dir`은 데이터 root와 분리한 절대 경로로 고르고 모든 continuation에
동일하게 사용한다. 한 페이지로 끝나면 snapshot을 만들지 않고 cursor 읽기는
생성이나 prune을 하지 않는다. `--columns`는 명시 text 출력에만 적용하며,
effective JSON과의 조합·중복 열·없는 열은 오류다. JSON의 필수 필드는 열 선택으로 제거하지 않는다.

## 복구 행동

`ready`는 실제 값이 바인딩되고 registry의 문법·정적 입력 계약을 통과한 argv다.
일반적인 새 작업 안내는 `--request-id`를 생략하고, 실행할 때 UUID를 생성해 반환한다.
원 요청 재시도에는 원 UUID를 유지하고, preview에 묶인 apply 안내도 준비된 UUID를 유지한다.
root 옵션은 명령 앞, 명령 옵션은 child `--` 앞에 있다.

`needs_input`은 비밀·파일·원 stdin처럼 다시 공급할 입력이 필요하다는 뜻이다.
가짜 적용 명령을 출력하지 않는다. `inputs`는 필요한 입력과 방법, `input_steps`는
실제로 실행할 수 있는 조회·help 경로다. `bindings`는 원 UUID·revision·scope 같은
공개 재시도 조건을 보존한다. agent/CI/pipe에서 숨겨진 prompt를 열지 않는다.
`manual`은 외부 도구나 사람이 수행할 단계다. help의 일반 예시는 template로 구별한다.

native vault가 잠겼거나 OS 인증창이 필요하면 실제 `devtools vault unlock --interactive`
명령을 안내한다. 이 명령은 사용자의 desktop session에서 인증창을 허용한다.
기본 `vault unlock`은 OS 인증창을 허용하지 않아 `key_interaction_required`로 실패할 수
있다. passphrase vault는 별도로 비공개 stdin/file 입력을 요구한다. `vault status`는
상태 확인만 하며 잠금을 해제하지 않는다. 키 없음·접근 거부·backend 불가 오류는
각각 구분하고 초기화나 backend 변경으로 자동 복구하지 않는다.

preview의 ready 적용 명령은 제공한 공개 입력과 child argv를 보존한다. 입력이나
설정이 바뀌면 digest/revision 대조에서 막히므로 새 preview가 필요하다.
비밀 입력을 되살리기 위해 일반 조회에 영구 snapshot을 만들거나 vault를 unlock하지 않는다.

## 보관과 유지보수

살아 있는 실행, 불명 결과, unresolved 최신 실패와 복구 참조는 보존한다.
해결된 상세 이력은 확인된 종료 이후 30일, 캡처 로그는 종료 이후 7일이 기준이다.
일반 조회는 삭제하지 않는다. 관련 쓰기 이후 제한된 owned 유지보수 단계가 다음 위치를
저장해 이어가며, 쓰기가 없으면 자동 정리도 진행하지 않는다.

```sh
devtools cleanup maintain --dry-run --limit 32
devtools cleanup maintain --limit 32
```

첫 명령은 다음 처리분의 적격 사유를 확인한다. 두 번째는 한 번의 제한된 처리다.
`has_more`가 있으면 후속 처리를 이어갈 수 있다. protected 자료를 확인할 권한이 없으면
verification_pending을 표시한다. 공개 자료와 인증된 자료의 검증 수준을 혼동하지 않는다.
새 daemon이나 외부 provider 삭제를 수행하지 않는다.

현재 서비스 요약과 요청 identity는 남는다. 무기한 재생·복원 계약이 필요한 본문은
공유 압축 저장소에 유지하므로 모든 자료가 일정한 용량으로 줄어드는 것은 아니다.
전달 중·unknown payload는 유지하고 확정 종료·실제 expiry만 인증된 쓰기에서 정리한다.
사용자 task·정의에는 TTL을 적용하지 않는다. 수동 cleanup 보관함은 기존 명시적
restore/purge와 30일 purge 제한을 유지한다.

형식 전환은 `doctor --storage --fix`와 설치·update의 기존 준비 hook에서 수행한다.
일반 조회는 옛 자료를 조용히 바꾸지 않고 같은 root의 준비 행동을 반환한다.
doctor 전환 자체가 TTL 정리나 archive purge를 실행하지는 않는다.
