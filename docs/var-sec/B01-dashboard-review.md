# B01 dashboard bundle 관리·import 리뷰

기존 values 화면과 edit dialog를 재사용한다. 묶음 생성·멤버 일괄 교체·rename·remove를 같은 revision/UUID API로 연결하며 값은 복사하지 않는다. 멤버 입력은 값 이름 목록뿐이고 secret 원문은 표시하지 않는다. 선택한 env와 무관한 이름 묶음이라는 안내, 전체 profile 기본 선택, empty/no-bundle 상태, 원본 값을 보존하는 삭제 안내를 제공한다.

import dialog는 선택적 bundle 이름을 받아 미리보기에서 target을 보여 주고 기존 공개/secret 분류 선택과 함께 원자적으로 반영한다. 기존 keep/overwrite 의미와 unknown-response 재시도는 그대로다. 잠긴 상태는 기존 mutation 차단 규칙을 공유한다. 잘못된/중복/없는 이름은 dialog 오류로 알리고 stale revision은 서버에서 전체 거절한다.

| 리뷰 영역 | 상태·근거 |
| --- | --- |
| 실제 CRUD API·공유 ID·import preview/apply | reviewed/pass, 실제 Chromium 생성→CLI membership 대조→invalid bulk 거절→public import로 멤버 추가→rename |
| keyboard·label·focus·잠금 | reviewed/pass, native labeled fields/buttons/dialog, Enter로 생성 시작, 저장 후 새/rename 대상 버튼 focus 복구, profile/tab 변경 후 focus를 빼앗지 않음, 기존 lock/unlock keyboard 회귀 pass |
| 긴 이름·narrow layout | reviewed/pass, 128자 이름과 390px/1280px 화면, 묶음 영역 수평 overflow 없음, 실제 screenshot 시각 확인. 이름을 버튼에 반복 표시하지 않고 accessible name으로만 구분 |
| 원문·XSS·보안 상태 | reviewed, textContent/기존 API helper, 원문 없는 이름·ID만 사용, passive unverified metadata 표시와 secret input/lock 흐름 유지 |
| 오류·재시도·기존 화면 | reviewed/pass, 기존 33개 dashboard JS 검사, Go dashboard 검사(1.274s), 실제 Chromium 2개 현재 checkout 시나리오(8.6s) |

로컬 기본 Playwright cache에 실행 파일이 없어 첫 실행은 시작 실패였다. 프로젝트가 고정한 Playwright의 Chromium을 /tmp/devtools-values-playwright에 설치하고 현재 repository/scripts만 대상으로 하는 임시 config로 재실행했다. 다른 worktree 시나리오를 현재 변경 검사로 세지 않았다. UI 기능/키보드/long-name 검사는 실제 현재 binary에서 pass했으며 accessibility 전체 인증이나 새 색 대비 측정으로 표현하지 않는다. 최종 screenshots는 /tmp/devtools-values-plan/B01-bundle-ui-focused-{mobile,desktop}.png, 로그는 B01-dashboard-bundle-browser-focused.log에 있다.

이번 하위 작업에 미해결 적격 결함은 없다. managed CLI override와 전체 B01 review/B03 native 통합은 별도이며 이 커밋만으로 B01을 완료하지 않는다.
