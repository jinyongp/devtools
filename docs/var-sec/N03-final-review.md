# N03 실제 Linux 수용과 최종 범위 리뷰

검증 source는 85e72de이며 [Actions run](https://github.com/jinyongp/devtools/actions/runs/39244830924)의
`vault-linux` job 118034163439가 성공했다. 이 task의 결과이며 전체 run/A12/Z01
성공을 뜻하지 않는다. [실제 원시 test events와 logout 결과](evidence/N03-native-85e72de.json)를 보존한다.

| 필수 영역 | 상태와 실제 근거 |
| --- | --- |
| 기존 서비스 없음·서비스 중지·자동 activation 금지 | reviewed/pass, synthetic private real D-Bus에서 absent/stopped required tests 모두 pass |
| 실제 native 저장/조회 | reviewed/pass, 실제 GNOME typed vault item roundtrip |
| collection 잠금·명시 해제·값 보존 | reviewed/pass, silent unwrap 거절 후 같은 item의 키 일치 |
| 실제 UID 경계 | reviewed/pass, root/65534의 두 실제 process에서 client/server 모두 거절 |
| PAM/logind logout·D-Bus/runtime 종료·새 login | reviewed/pass, 소유 계정의 실제 session/user termination 후 runtime 소멸과 새 OS/vault session, 이전 warm key 사용 거절 |
| 실제 native CLI 복구 | reviewed/pass, 새 session의 잠긴 collection 거절 후 명시 해제·같은 secret의 masked 실행 |
| fixture 신원·정리·원문 보호 | reviewed/pass, 새 계정/User/Name/Service 대조·소유 unit/PAM/HOME만 정리·pass artifact는 cleanup 뒤 작성 |

이전 실패의 원인은 GNOME fixed text/plain MIME 처리, 기존 daemon의 control unlock
가정, private fixture 경로, root 명령에 잘못 붙인 --profile이었다. 각 원인을 별도
커밋으로 수정하고 관련 호출자·오류·cleanup을 다시 검토했다. native wrapper는 실제
required test pass와 skip/fail 없음을 검증한다. source compile이나 fake adapter를
이 native 결과로 대체하지 않았다. 최종 범위 리뷰에서 N03의 미해결 적격 결함은 없다.

logout은 desktop UI를 흉내 낸 것이 아니라 새 PAM session을 logind에 등록하고
loginctl로 그 소유 session/user를 실제 종료한 방식이다. 임의 D-Bus mid-request kill,
GUI screen lock·suspend·사용자 lingering 정책 전체의 동일 동작까지 증명하지 않는다.
동일 사용자 sandbox·OS/root 공격 방어·이미 앱에 전달한 환경의 회수도 보장하지 않는다.
원래 WSL restart/multi-terminal·다른 OS IPC·N05의 fault matrix와 A12 성능/전체 회귀는
각각 별도로 유지하며, 이 완료로 면제하지 않는다.
