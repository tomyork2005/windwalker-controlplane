package dao

import "control-plane/internal/domain"

func FromDaoPlanToDomainPlan(plan PlanDAO) (domain.Plan, error) {
	money, err := domain.NewMoney(plan.Amount, plan.Currency)
	if err != nil {
		return domain.Plan{}, err
	}

	return domain.Plan{
		ID:       plan.ID,
		Name:     plan.Name,
		Region:   plan.Region,
		Protocol: plan.Protocol,
		Money:    money,
		Archived: plan.Archived,
	}, err
}
