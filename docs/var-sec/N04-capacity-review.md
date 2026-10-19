# N04 IPC 한계·폭주 수용 보강 리뷰

동일 사용자 프로그램은 endpoint를 읽고 wire protocol을 구현하면 warm root key를 받을 수 있다. 실제 별도 daemon에 typed SessionClient.Use를 거치지 않는 직접 handshake/use를 보내 같은 키가 전달됨을 확인한다. 키는 비교 뒤 clear하고 출력하지 않는다. 이 검사는 방어 성공이 아니라 기존 same-user trust 한계와 문서의 일치다. Keychain 앱 ACL을 넓히는 변경은 없다.

실제 transport 연결 8개에서 hello를 받아 admission을 확인한 채 요청을 보류한다. 추가 연결이 timeout이 아닌 prompt rejection인지 검사하고, held 연결을 닫은 뒤 상태 조회가 다시 성공해야 한다. timeout/retirement는 bounded이고 직접 소유 연결만 닫는다. malformed frame 검사와 합쳐 frame allocation·worker 수의 경계를 검증하며 host 전체 DoS 면역을 주장하지 않는다.

별도로 같은 실제 passphrase preset을 사용하는 KeySession에 barrier로 8개 잘못된 암호 시도를 시작한다. 실제 Protector.Unwrap 호출을 감싼 카운터의 동시 최대가 1이고 모든 요청이 무결성 실패인지 확인한다. 이는 실제 Argon2 작업의 직렬화 검사이며 native API 인증 UI 시험이 아니다. fake clock idle와 실제 OS cold reopen 검사를 구분한다.

리뷰 영역: 같은 사용자 위협 모델·원문 로그 방지·직접 RPC identity·capacity admission 증거·worker 해제/오류 복구·실제 KDF 호출과 bounded context·defer 정리를 확인했다. Linux에서 세 검사 race pass(2.334s). Windows/Darwin compile과 실제 양 architecture suite는 후속 CI 전 pending이다. N04 완료를 이 로컬 결과만으로 선언하지 않는다.
