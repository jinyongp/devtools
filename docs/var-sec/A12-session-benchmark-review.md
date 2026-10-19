# A12 cold passphrase와 warm key session 측정 준비

생산 Argon2id 64MiB/t3/p4를 그대로 사용하는 in-process key session benchmark다.
새 profile/vault/password fixture 생성과 초기 unlock은 timing에서 제외한다. cold case는
각 반복 전에 실제 manual lock을 완료한 뒤 Unlock 전체를 측정한다. unlock에 필요한
파일 읽기·KDF·검증·durable manual latch 해제는 포함하며 lock 쓰기는 제외한다. warm
case는 Use와 반환 material의 Destroy를 측정한다. matcher/CLI/IPC/native OS API는
대상 밖이다. 따라서 이 수치를 실제 CLI 실행 latency나 native unwrap으로 표시하지 않는다.

KDF wrapper는 실제 argon2.IDKey를 같은 입력과 파라미터로 호출하고 횟수만 센다.
각 cold unlock은 1회, warm Use는 0회여야 한다. 측정 뒤 원래 키와 일치하는지도 검사한다.
benchmem은 Go allocation byte와 횟수이며 peak RSS·키 잔존 시간·OS zeroization이 아니다.
private root를 canonical path로 만들고 각 세션·합성 passphrase/key material을 정리한다.

리뷰 영역: 실제 preset/원문 비출력·timing 경계·manual latch 의미·warm 재사용·관측값 소비·
메모리 지표와 한계·local/native 구분을 확인했다. 실제 preset 회귀와 두 case 1회 smoke
pass. 본 smoke 수치를 성능 결과로 채택하지 않는다. clean commit에서 5회 calibrated
반복을 실행하여 raw stdout을 보존하고 A12의 다른 축은 별도로 유지한다.
