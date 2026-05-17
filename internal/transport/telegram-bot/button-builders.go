package bot

import (
	"control-plane/internal/model"
	"fmt"
	"sort"
	"strings"
)

// regionDisplay returns "<flag> <human-readable name>" for a region code.
// Falls back to the raw code if unknown.
var regionDisplay = map[string]string{
	"lv": "🇱🇻 Латвия",
	"nl": "🇳🇱 Нидерланды",
	"de": "🇩🇪 Германия",
	"fi": "🇫🇮 Финляндия",
	"fr": "🇫🇷 Франция",
	"us": "🇺🇸 США",
	"uk": "🇬🇧 Великобритания",
	"gb": "🇬🇧 Великобритания",
	"pl": "🇵🇱 Польша",
	"se": "🇸🇪 Швеция",
	"no": "🇳🇴 Норвегия",
	"ee": "🇪🇪 Эстония",
	"lt": "🇱🇹 Литва",
	"jp": "🇯🇵 Япония",
	"sg": "🇸🇬 Сингапур",
	"tr": "🇹🇷 Турция",
}

func regionLabel(code string) string {
	if v, ok := regionDisplay[strings.ToLower(code)]; ok {
		return v
	}
	return strings.ToUpper(code)
}

func buildRegionBtns(plans []*model.Plan) map[string]any {
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

	rows := make([][]map[string]any, 0, len(regions)+1)
	for _, region := range regions {
		rows = append(rows, []map[string]any{iconCallbackBtn(regionLabel(region), string(actPickRegion), region, "")})
	}
	rows = append(rows, []map[string]any{iconCallbackBtn("В меню", string(actBackToMain), "", iconHome)})
	return iconKeyboard(rows...)
}

func buildProtocolBtns(plans []*model.Plan, region string) map[string]any {
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

	rows := [][]map[string]any{
		{iconCallbackBtn("Back to regions", string(actPickRegion), "__back__", iconBack)},
	}
	row := make([]map[string]any, 0, 3)
	for i, pr := range protos {
		row = append(row, iconCallbackBtn(strings.ToUpper(pr), string(actPickProtocol), pr, ""))
		if len(row) == 3 || i == len(protos)-1 {
			rows = append(rows, row)
			row = make([]map[string]any, 0, 3)
		}
	}
	rows = append(rows, []map[string]any{iconCallbackBtn("В меню", string(actBackToMain), "", iconHome)})
	return iconKeyboard(rows...)
}

// One in row
func buildDurationBtns(plans []*model.Plan, region, proto string) map[string]any {
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

	rows := [][]map[string]any{
		{iconCallbackBtn("Back to protocols", string(actPickProtocol), "__back__", iconBack)},
	}
	for _, p := range filtered {
		title := fmt.Sprintf("%s — %d %s", p.Name, p.Money.Amount, p.Money.Curr)
		rows = append(rows, []map[string]any{iconCallbackBtn(title, string(actPickDuration), p.ID, "")})
	}
	rows = append(rows, []map[string]any{iconCallbackBtn("В меню", string(actBackToMain), "", iconHome)})
	return iconKeyboard(rows...)
}

func buildPaymentMethodBtns(methods []*model.PaymentMethod, planID string) map[string]any {
	rows := [][]map[string]any{
		{iconCallbackBtn("Back to durations", string(actPickDuration), "__back__", iconBack)},
	}

	for _, method := range methods {
		payload := packMethodPayload(method.ID, planID)
		rows = append(rows, []map[string]any{iconCallbackBtn(method.Name, string(actPickPaymentMethod), payload, "")})
	}

	rows = append(rows, []map[string]any{iconCallbackBtn("В меню", string(actBackToMain), "", iconHome)})
	return iconKeyboard(rows...)
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
