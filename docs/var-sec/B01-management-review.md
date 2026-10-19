# B01 bundle 일괄 관리·import·CLI 하위 작업 리뷰

ModernStore의 기존 revision/UUID/암호화 receipt/원자적 publish에 bundle set/rename/remove를 연결한다. 한 요청은 전체 membership을 공유 ValueID로 교체하며 기존 값과 slot version을 유지한다. metadata view는 bundle ID/member ID와 item ValueID만 추가하고 secret 원문을 노출하지 않는다. passive list는 integrity_verified=false를 유지하며 잠금 해제하지 않는다.

import --bundle은 값 편집과 기존 membership의 합집합을 같은 profile transaction으로 반영한다. 새 member만 추가해도 changed=true이고 이미 같은 membership/값이면 no-op이다. 기존 CLI는 선택된 env layer의 충돌/overwrite 규칙, dashboard는 기존 inherited 값을 보존하는 규칙을 유지한다. 둘을 구분하는 LayerOnly 입력은 생략 시 기존 dashboard 동작이며 old receipt의 JSON fingerprint를 바꾸지 않도록 omitempty다.

| 범위 | 상태·근거 |
| --- | --- |
| atomic edit·ID·membership·같은 UUID·stale edit | reviewed/pass, 오류 뒤 revision 불변·변경 UUID input 거절·원래 결과 replay·rename identity 유지 |
| atomic import·membership-only 변경·preview | reviewed/pass, union 후 실제 capture member 수 확인, preview/실패는 값과 bundle 모두 불변 |
| CLI schema/list/set/rename/remove·원문 없는 결과 | reviewed/pass, 별도 bundle 명령 등록, revision+UUID 입력·bulk validation·metadata 결과 |
| 기존 CLI conflict/env와 dashboard import | reviewed/pass, bundle 옵션으로 overwrite 거절을 무시하지 않음, 기존 LayerOnly 없는 요청과 caller 유지 |
| 저장/암호화·키 수명·호환 receipt | reviewed/pass, 기존 protected/public scope·key destroy·manifest publish와 encrypted receipt 경로 재사용, 새 optional 필드 omitempty |

전체 values/cli/dashboard race 검사 pass(64.647/49.089/3.771s). 대상 bundle API/CLI race 검사도 pass(1.273/1.129s). 설치된 vault의 bundle import→frontend 공개 값만 주입→backend secret 마스킹 검사도 pass. UI 편집 및 managed CLI override 지속은 후속 B01 범위로 남기며 이 커밋으로 B01 완료를 선언하지 않는다.
