# B01 bundle 모델·선택 하위 작업 리뷰

B01 전체 중 authenticated model과 resolution만 구현했다. CLI/project/dashboard/import 통합은 진행 중이며 이 커밋으로 B01이나 B03을 완료하지 않는다.

| 범위 | 상태·근거 |
| --- | --- |
| 공통 var/sec membership·공유 identity | reviewed/pass, SetBundle은 ValueID만 참조, value/slot/version/ciphertext 불변, 두 묶음에서 수정 값 공유 |
| bulk atomicity·중복/없는 값·순서만 변경 | reviewed/pass, clone/verify/sign/encode 성공 뒤 교체, 오류 시 원본 byte 일치, 순서만 다른 요청 no-op |
| bundle rename/remove | reviewed/pass, ID 유지·이름 충돌 거절·원본 값 유지·삭제 후 missing 오류 |
| all/none/named·env 분리·expired override | reviewed/pass, 기본 all, none도 env 검증, 선택 밖 expired 값 제외, 선택된 expired override는 fail |
| manifest 전체 인증·public-only | reviewed/pass, none 포함 모든 선택 전에 전체 Verify; sealed bundle 변조 거절, public-only no-key 선택 |
| 기존 호출자·저장/전송 형식 | reviewed/pass, 기존 ResolveValues는 all로 위임, 기존 signed manifest/논리 전송 bundle 형식 유지 |

관련 bundle 검사와 전체 internal/vault race 검사가 통과했다. 최종 membership lookup은 값 수×member 수 탐색 대신 map을 사용하며 수정 후 대상 검사를 다시 통과했다. 기존 전체 범위에서 미해결 적격 결함은 없다. OS 권한·session runtime·새 공개 CLI는 이 하위 작업에서 변경하지 않았다.
