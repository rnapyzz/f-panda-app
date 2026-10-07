package report

// 楽観・基準（加重見込）・悲観の算出（docs/plan.md「2.8」）。
// 金額は満額で保存し、集計のときに内訳（なければ施策）の確度の段階と見通しの種類で算出する。実績の月はどれも実績の金額。
//
//	楽観: ベース・アドオンはすべて計上、ダウンサイドは計上しない
//	基準: 金額 × 段階の標準の確率
//	悲観: ベース・アドオンは確度の高い段階（標準の確率が highRate 以上。初期値では A・B）だけ計上、ダウンサイドは全額を計上

// highRate は「確度の高い段階」とみなす標準の確率の下限（初期値の A 確定 100%・B 高 80%）。
const highRate = "0.8"

var measures = map[string]bool{"full": true, "weighted": true, "optimistic": true, "pessimistic": true}

// measureJoins は scenario_amounts（別名 b）に、段階（cl）と内訳（l）を結びつける JOIN。
const measureJoins = `
	JOIN activities ma ON ma.id = b.activity_id
	LEFT JOIN activity_lines l ON l.id = b.line_id
	JOIN confidence_levels cl ON cl.code = COALESCE(l.confidence_level, ma.confidence_level)`

// measureSum は、指標の金額を合計する SQL の式（円未満は四捨五入）。
func measureSum(measure string) string {
	switch measure {
	case "weighted":
		return "CAST(ROUND(SUM(CASE WHEN b.kind = 'actual' THEN b.amount ELSE b.amount * cl.rate END)) AS CHAR)"
	case "optimistic":
		return "CAST(SUM(CASE WHEN b.kind = 'actual' THEN b.amount WHEN COALESCE(l.outlook, 'base') = 'downside' THEN 0 ELSE b.amount END) AS CHAR)"
	case "pessimistic":
		return "CAST(SUM(CASE WHEN b.kind = 'actual' THEN b.amount WHEN COALESCE(l.outlook, 'base') = 'downside' THEN b.amount WHEN cl.rate >= " + highRate + " THEN b.amount ELSE 0 END) AS CHAR)"
	}
	return "CAST(SUM(b.amount) AS CHAR)"
}
