# N02 Windows x64·ARM64 native 최종 리뷰

실제 source **44896ec73ebdebe5227b6d0c4748e670df288c7a**의 [Actions](https://github.com/jinyongp/devtools/actions/runs/39248889539)에서 두 architecture의 전체 Go suite, 설치된 product journey, PS5/PS7 installer, ARM의 x64 installer 실행과 x64 race가 모두 성공했다. [실제 이벤트와 설치 결과](evidence/N02-native-44896ec.json)에 job ID와 source를 보존한다. ARM64는 native contention이며 Go race 통과로 표현하지 않는다.

| 범위 | 최종 상태·근거 |
| --- | --- |
| 실제 DPAPI roundtrip·vault entropy 분리·손상·취소 | reviewed/pass, TestNativeDPAPIUserRoundtrip |
| 다른 SID가 받은 ciphertext의 해제 거절 | reviewed/pass, 실제 thread identity 적용·대조와 원래 신원 복귀·동일 키 재해제 |
| 파일/디렉터리·pipe ACL·reparse/hardlink 경계 | reviewed/pass, native platformfs suite와 실제 other-SID private pipe open 거절 |
| 비대화형 연결·manual latch·nopassphrase 명시 해제 | reviewed/pass, 실제 기본 root의 packaged CLI → DPAPI helper → 별도 Node child masked run |
| native 데이터가 남은 실제 self-update | reviewed/pass, old worker retirement/lease drain 뒤 실제 다른 version으로 updater 실행, cold native unlock·같은 vault/secret |
| native 경합·x64 race·관리 실행·전송 회귀 | reviewed/pass, 양 architecture 전체 suite 및 product; x64만 race |
| 합성 신원·기존 root 거절·출력·정리 | reviewed/pass, disposable CI 계정의 새 기본 root만 소유, cleanup 뒤 성공 artifact |

리뷰는 DPAPI 오류/identity 복귀, private filesystem 경계, helper와 root guard, update/old cache 경계, 실행·전송 호출자와 cleanup을 포함했다. stop 응답 유실의 Changed 계약 결함은 bc76f45로 수정하고 native 양 architecture suite와 x64 race로 재검사했다. native 자료를 유지한 update는 de53a72에서 처음 통과했고 현재 source에서도 다시 통과했다. 이전 단일 installer의 null 실패는 재발하지 않았지만 진단만 추가했으므로 원인을 수정했다고 주장하지 않는다. 취소된 이전 run을 통과로 계산하지 않는다.

현재 N02 범위의 미해결 적격 결함은 없다. 현재 개발 호스트의 기본 vault에는 native fixture를 실행하지 않았다. 실제 GitHub native runner 결과이며 물리 PC 전체 배포 조건까지 뜻하지 않는다. N04의 양방향 IPC/WSL, N05 통합 fault, A12 전체 수용 및 Z01은 독립적으로 유지한다. 준비 문서의 pending은 이 결과로 해소한다.
