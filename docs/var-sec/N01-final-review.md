# N01 macOS 설치·업데이트·Keychain 최종 리뷰

실제 source **44896ec73ebdebe5227b6d0c4748e670df288c7a**, [Actions run](https://github.com/jinyongp/devtools/actions/runs/39248889539)의 Intel job 118047629770과 ARM job 118047629755가 성공했다. [원시 native events와 설치 결과](evidence/N01-native-44896ec.json)를 보존한다. 전체 run/A12/Z01의 완료와 구분한다.

| 범위 | 최종 상태·근거 |
| --- | --- |
| 실제 Keychain wrap/unwrap·vault 분리·손상 | reviewed/pass, required native test, skip 없음 |
| OS collection 잠금·비대화형 거절·명시 해제 | reviewed/pass, raw native와 실제 설치 CLI 두 경로 |
| 실제 build/install·서명 검증 | reviewed/pass, 두 ad-hoc signed production-build 합성 package와 실제 installer |
| 실제 self-update·서명 경계·데이터 보존 | reviewed/pass, old key session을 retirement/lease로 종료 후 다른 signature의 접근 거절, 실제 updater로 이전 signature 복구 뒤 같은 vault/secret 실행 |
| 앱 권한·사용자 개입 정책 | reviewed, 기존 creator-app ACL 유지. 평소 reusable session, 서명 변경 뒤 필요한 OS 승인만 명시 --interactive. 사용자 위임을 앱 전체 decrypt 권한 확대 승인으로 해석하지 않음 |
| 합성 자료·출력·소유권·정리 | reviewed/pass, secret stdin/masked run, wrapper-owned keychain만 사용·원래 default/search 복구·synthetic HOME 삭제 뒤 성공 artifact |

앱 전체 decrypt 권한 확대안은 자동 승인 검토에서 거절되어 적용하지 않았으며, 사용자의 ‘가장 좋은 방안’ 위임에 따라 기존 앱별 보호를 선택했다. 현재 source에는 해당 권한 확대가 없다. 이전 준비 문서의 pending은 이 실제 결과로 해소한다.

이 리뷰는 product adapter, 설치/update 호출자, signature/old session 경계, typed denial와 명시 복구, 문서·cleanup을 다시 확인했다. 미해결 적격 결함은 없다. headless CI가 사람의 승인 UI 클릭을 수행했다고 주장하지 않으며 Developer ID/notarization/Gatekeeper 전체 정책도 검증 범위에 포함하지 않는다. 이전 버전 복구는 명시 binary 복구이며 secret/config 자동 rollback이 아니다. 다른 OS·자동 OS 재해제·통합 fault matrix·전체 최종 리뷰는 별도 작업으로 유지한다.
