package service

//go:generate minimock -i ProcessPayment -o ./mocks/process_payment_mock.go -s _mock.go
//go:generate minimock -i ProcessStorage -o ./mocks/process_storage_mock.go -s _mock.go
//go:generate minimock -i Outbox -o ./mocks/outbox_mock.go -s _mock.go
//go:generate minimock -i ShopPayment -o ./mocks/shop_payment_mock.go -s _mock.go
//go:generate minimock -i ShopStorage -o ./mocks/shop_storage_mock.go -s _mock.go
//go:generate minimock -i ShopOutbox -o ./mocks/shop_outbox_mock.go -s _mock.go
