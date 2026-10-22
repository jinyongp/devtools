# CLI 진단 개선 요구사항

[문서 목록](README.md) · [설정 모델](cli-config-redesign.md) · [편집 계약](config-edit-contract.md) · [진단 계약](diagnostic-contract.md)

이 문서는 protocol 10의 공개 안내와 회귀 검증에 사용하는 요구사항이다.
D01–D19와 C01–C03은 동작·검증을 연결하는 안정된 식별자이며 테스트 통과 여부 자체를 뜻하지 않는다.
설치본의 명령 존재와 protocol은 `devtools version`, `devtools schema COMMAND`로 확인한다.

실패 응답은 작업·대상·관찰한 조건·수정 위치·검사 범위·다음 조치·변경 효과를 설명해야 한다.
민감한 본문·전체 환경·private storage 경로와 credential은 공개하지 않는다.

## 실행과 진단 범위

### D01 · 실행 파일과 독립적인 필수 조건을 함께 검사한다

argv[0]이 없어도 독립적인 도구·키 검사를 계속한다. 대표 오류에는 executable 이름을, checks에는 수집한 결과를 보존한다. 선행 조건 때문에 확인할 수 없는 검사는 unknown과 이유를 제공한다.

### D02 · 실패한 대상과 doctor의 검사 범위를 일치시킨다

기본 doctor는 전체 명령을 검사한다. 복구 안내는 실패한 명령의 dir/profile/env/bundle/mode를 유지한다. 보고서에 선택 범위·명령별 결과·집계와 미확인 수를 포함한다.

### D07 · 도구 검사에서 기대값과 실패 원인을 보존한다

도구 이름·선언 executable·기대/관찰 버전·선언 위치를 구분한다. 미발견·실행 권한·버전 불일치·probe 실패·timeout·출력 상한을 reason으로 설명한다. PATH 미발견을 설치 부재로 단정하거나 probe 원문을 노출하지 않는다.

## 명령 입력과 설정 검증

### D03 · 명령·옵션 오타에 해당 입력과 탐색 경로를 제공한다

입력 위치·명령군·가까운 후보와 해당 help/schema 경로를 제공한다. 자동 교정하거나 후보를 실행하지 않는다.

### D04 · 입력 제약을 사람이 이해할 수 있게 설명한다

필드·type/enum/pattern·길이 규칙과 source를 제공한다. 민감한 입력값을 복사하지 않는다. 옵션 값이 다른 옵션을 삼키지 않도록 경계를 검사한다.

### D05 · TOML 문법 오류와 알 수 없는 필드를 구분한다

syntax_error, unknown_field, duplicate_field와 의미 오류를 구분한다. 확보한 key path·byte 위치·행/열을 제공하고 원문 파일이나 parser의 입력 조각은 복사하지 않는다.

### D06 · 의미 검증 결과에 선언 이름과 위치를 포함한다

참조·중복·도구 버전 충돌을 관련 resource와 source에 연결한다. 후보 전체를 검증하고 source 위치·code 순으로 안내한다.

### D14 · JSON body 오류에 구조화된 필드 경로를 제공한다

입력 role·reason·문자열 배열 path와 실제 확보한 byte 위치를 제공한다. 잘못된 본문·secret·자유 형식 decoder 오류를 노출하지 않는다. 설정 편집은 strict JSON 경계를 적용한다.

### D18 · schema와 runtime이 입력 규칙을 공유한다

설정·mutation·response·diagnostic의 정본은 [공통 schema](../internal/configcontract/schema.json)다. 구조 검증 뒤 참조·실행 의미·filesystem은 공유 도메인 validator로 검사한다. parser의 필수·반복·본문 입력 선택 규칙과 discovery를 함께 검증한다.

### D19 · help에서 완성된 호출을 배울 수 있게 한다

모든 명령에 호출 예제를, JSON body 명령에는 유효한 body_example을 제공한다. help와 schema가 같은 metadata를 사용한다. 예제는 가상 식별자를 사용하고 사용자 입력이 필요한 항목을 설명한다.

## 도메인 오류와 복구

### D08 · 조건 code와 도메인 문맥을 일관되게 유지한다

공통 ErrorDetails가 canonical operation·phase·target·reason을 제공하고 causes에 하위 조건을 보존한다. code를 추출하려고 원문 메시지를 파싱하지 않는다.

### D09 · 호출자가 사용할 수 있는 복구 경로만 제시한다

remedy는 현재 명령이 받는 인자와 선택 범위에 맞아야 한다. required_inputs와 effect를 명시하고 입력값이나 권한을 추정하지 않는다.

### D10 · 도메인 오류에 대상·상태·단계를 보존한다

process·route·port·preview·request 식별자와 공개 상태를 유지한다. 부분 성공은 outcomes로, 하위 실패는 causes로 보존한다. 같은 request의 재시도 규칙과 나중 writer의 결과를 유지한다.

### D11 · 파일 읽기 실패와 내용·인증 실패를 구분한다

caller 입력의 role과 안전한 I/O reason·numeric OS code를 유지한다. 없는 파일을 잘못된 암호로 표시하지 않는다. 암호 오류와 ciphertext 손상을 구분하지 않는 인증 경계는 보존한다.

### D12 · 값 변경 오류에 선택 범위와 재조회 경로를 제공한다

profile/env/key·revision 등 공개 식별자와 조회 remedy를 제공한다. 값은 노출하지 않고 오래된 변경 의도를 새 revision에 자동 적용하지 않는다.

### D13 · vault와 외부 실행의 실패한 작업을 구분한다

operation·phase와 보호 상태 조회 경로를 유지한다. 키가 없다는 이유만으로 vault나 저장소를 재생성하지 않는다. 세션 credential과 실행 context는 공개하지 않는다.

### D17 · installer와 update에 안전한 단계별 reason을 제공한다

설치 단계는 facts.stage, OS 실패는 typed reason과 numeric code로 제공한다. 설치 결과의 effects와 retry를 보존한다. 예외 원문·credential·private 경로나 helper stderr를 전달하지 않는다.

## 집계와 화면

### D15 · diagnostics 집계에서 public 안내를 유지한다

도메인별 허용 사실을 공통 진단으로 집계한다. checks·causes·remedies·효과와 상태를 보존하며 raw details 전체를 내보내지 않는다.

### D16 · dashboard가 동일한 진단을 소비한다

dashboard API와 화면은 공통 public facts·실패 검사·remedy·부분 성공 ID를 사용한다. raw record·원문 log·storage path·credential은 표시하지 않는다.

## 설정 작성 모델

### C01 · 도구 정의 재사용과 전체 명령의 요구사항을 분리한다

tools는 사양 등록, presets는 재사용할 선택, defaults는 모든 명령의 선택이다. 등록을 전역 필수 조건으로 승격하지 않으며 migration 전후 모든 명령의 해석 결과가 같아야 한다.

### C02 · 검증된 설정 편집 경로를 제공한다

create-only add·partial update·명시적 remove·literal rename·atomic batch를 제공한다. 같은 writer와 validator를 사용하고 revision·권한·주석 보존을 확인한 뒤 게시한다.

### C03 · 해석 결과의 출처와 변경 영향을 설명한다

command explain은 최종 값의 provenance와 미해결 runtime 입력을 제공한다. 편집 preview는 영향받는 모든 명령을 계산한다. 설정 조회는 저장 값·도구·network 상태를 실행하지 않는다.

## 검증 경계

[합성 계약 사례](../internal/configcontract/cases.json), Go provider 검사, CLI integration,
설치 release 시나리오와 native 플랫폼 검사는 서로 다른 경계를 검증한다.
Linux의 성공이나 cross compilation을 Windows·macOS 파일 게시·ACL 검증으로 간주하지 않는다.
비노출 검사는 오류·집계·dashboard와 installer의 각 공개 경계에서 수행한다.
