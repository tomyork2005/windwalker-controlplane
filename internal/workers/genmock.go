package workers

//go:generate minimock -i Storage -o ./mocks/storage_mock.go -s _mock.go
//go:generate minimock -i AgentSubscribeService -o ./mocks/agent_subscribe_service_mock.go -s _mock.go
//go:generate minimock -i TelegramSender -o ./mocks/telegram_sender_mock.go -s _mock.go
