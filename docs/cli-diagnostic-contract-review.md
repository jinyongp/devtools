# CLI 진단·입력 계약 후속 구현 검토

기준은 `7df9589fd322d7adf4090790b4ab5b74c3d6a22b` / v0.36.0 / protocol 16이다.
구현은 기존 main checkout에서 진행하며 공개 CLI 계약은 protocol 17로 올린다.
실행 상태와 최종 SHA·검증 로그·Actions 근거의 정본은 devtools profile의
workstream `1996f968-02e6-44bf-9dda-ae273ca09192`에 기록한다.

| 단위 | 검토한 계약과 직접 소비자 | 구현 commit |
| --- | --- | --- |
| U01 | 도메인 facts → Public → Failure schema → JSON/text, 0/false·비공개 값 대조 | `2a9f9e1` |
| U02 | edit parse/leaf/final-state 오류 → 실제 operation 위치, 원 workstream·coverage 안내 | `c590fae` |
| U03 | 정적 옵션의 명시 여부·활성 상태 → schema/runtime/help, canonical·alias preflight | `f2ab24c` |
| U04 | file/stdin 입력 선택 → 빈 경로·false·UUID replay, 암호 입력 selector | `4c2d5dc` |
| U05 | config Operation union → discriminator에 맞는 실제 실패·타입 facts·불변 파일 | `d96583f` |
| U06 | 설치 archive → 공개 오류 schema·JSON/text·PTY/ConPTY·복구 argv 실행 | 이 문서와 같은 단위 |
| U07 | 전체 변경과 직접 caller 재검토, 최종 main SHA의 fresh 통합검증 | 실행 근거 참조 |

독립 fixture는 규칙 정의에서 기대값을 생성하지 않는다. lower-level matrix는
operation 위치 0/1/2, 오류 종류, canonical/alias와 입력 source 조합을 검사한다.
native journey는 대표 사례와 실제 터미널을 검사하며 같은 matrix 전체를 중복하지 않는다.
공개 Failure schema 검증은 실제 설치 CLI가 만든 오류를 입력으로 받고,
`observed_exact`에 문자열을 넣은 음성 대조가 실패해야 한다.

U06의 실제 설치 검사에서 schema 안내에 `--isolated-root`가 붙어 조회 대상이
잘못되는 결함을 확인했다. schema 행동을 discovery로 선언해 저장소 옵션을 상속하지
않도록 수정했다. 원 workstream의 plan/context 조회는 같은 저장소 범위를 유지한다.
실패 뒤 task plan·값 revision·config bytes 불변, canary 비노출도 함께 검사한다.

최종 검증은 기존 `check:all`, 전체 `test:installed`, Release workflow의
`publish=false`, `reuse_validation=false`를 사용한다. 최종 SHA의 Linux/macOS,
Windows x64/ARM64, 실제 WSL2 재시작·터미널, native key backend, 배포물 identity와
12방향 profile exchange 결과를 별도로 확인해야 완료로 기록한다.
이 문서는 계획된 검사를 통과 결과로 대신하지 않는다.

envelope 1, 업무 저장 형식·journal·요청 fingerprint·private vault/runner wire는
유지한다. 만료 payload sweep과 spent receipt 압축은 계속 제외한다.
별도 시각 작업의 T05/T06, 실제 PC의 사용자 관찰과 공개 provider/system-network
수용을 이 검증의 성공으로 닫지 않는다. 정식 release 게시와 global CLI 교체는
이번 후속 실행 범위에 포함하지 않는다.
