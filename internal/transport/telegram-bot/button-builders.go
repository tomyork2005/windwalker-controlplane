package bot

import (
	"control-plane/internal/model"
	"fmt"
	tele "gopkg.in/telebot.v4"
	"sort"
	"strings"
)

func buildRegionBtns(plans []*model.Plan) *tele.ReplyMarkup {
	set := map[string]struct{}{}
	for _, p := range plans {
		if p.Archived {
			continue
		}
		if p.Region != "" {
			set[p.Region] = struct{}{}
		}
	}
	if len(set) == 0 {
		return nil
	}

	regions := make([]string, 0, len(set))
	for r := range set {
		regions = append(regions, r)
	}
	sort.Strings(regions)

	var kb tele.ReplyMarkup
	var rows []tele.Row
	row := make([]tele.Btn, 0, 3)
	for i, region := range regions {
		btn := kb.Data(region, string(actPickRegion), region) // Unique = действие, Data = region
		row = append(row, btn)
		if len(row) == 3 || i == len(regions)-1 {
			rows = append(rows, kb.Row(row...))
			row = row[:0]
		}
	}
	kb.Inline(rows...)
	return &kb
}

func buildProtocolBtns(plans []*model.Plan, region string) *tele.ReplyMarkup {
	set := map[string]struct{}{}
	for _, p := range plans {
		if p.Archived || p.Region != region {
			continue
		}
		if p.DriverType != "" {
			set[p.DriverType] = struct{}{}
		}
	}
	if len(set) == 0 {
		return nil
	}

	protos := make([]string, 0, len(set))
	for pr := range set {
		protos = append(protos, pr)
	}
	sort.Strings(protos)

	var kb tele.ReplyMarkup
	// back к регионам
	btnBack := kb.Data("⬅ Back to regions", string(actPickRegion), "__back__")

	rows := []tele.Row{kb.Row(btnBack)}
	row := make([]tele.Btn, 0, 3)
	for i, pr := range protos {
		btn := kb.Data(strings.ToUpper(pr), string(actPickProtocol), pr)
		row = append(row, btn)
		if len(row) == 3 || i == len(protos)-1 {
			rows = append(rows, kb.Row(row...))
			row = row[:0]
		}
	}
	kb.Inline(rows...)
	return &kb
}

// One in row
func buildDurationBtns(plans []*model.Plan, region, proto string) *tele.ReplyMarkup {
	filtered := make([]*model.Plan, 0)
	for _, p := range plans {
		if p.Archived {
			continue
		}
		if p.Region == region && p.DriverType == proto {
			filtered = append(filtered, p)
		}
	}
	if len(filtered) == 0 {
		return nil
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Money.Amount < filtered[j].Money.Amount
	})

	var kb tele.ReplyMarkup
	btnBack := kb.Data("⬅ Back to protocols", string(actPickProtocol), "__back__")

	rows := []tele.Row{kb.Row(btnBack)}
	for _, p := range filtered {
		title := fmt.Sprintf("%s — %d %s", p.Name, p.Money.Amount, p.Money.Curr)
		rows = append(rows, kb.Row(kb.Data(title, string(actPickDuration), p.ID)))
	}
	kb.Inline(rows...)
	return &kb
}

func buildPaymentMethodBtns(methods []*model.PaymentMethod, planID string) *tele.ReplyMarkup {
	var kb tele.ReplyMarkup
	var rows []tele.Row
	for _, method := range methods {
		payload := packMethodPayload(method.ID, planID)
		rows = append(rows, kb.Row(kb.Data(method.Name, string(actPickPaymentMethod), payload)))
	}
	kb.Inline(rows...)
	return &kb
}

func packMethodPayload(methodID, planID string) string {
	return methodID + "|" + planID
}

func unpackMethodPayload(payload string) (string, string) {
	parts := strings.SplitN(payload, "|", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return payload, ""
}
