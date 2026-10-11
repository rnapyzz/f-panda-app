-- 年度をまたぐ施策（docs/plan.md「2.26」「15」）。シナリオは翌年度の月の計画値も持てるようになるため、
-- scenario_amounts（年度の集計に使う）はシナリオの年度の12か月だけを返すようにする。翌年度の月は budget_facts から直接読む。

CREATE OR REPLACE VIEW scenario_amounts AS
SELECT b.scenario_id, b.activity_id, b.subject_id, b.line_id, b.target_month, b.amount,
       'plan' AS kind, b.source, b.is_provisional, b.provisional_reason
FROM budget_facts b
JOIN scenarios s ON s.id = b.scenario_id
WHERE (s.actual_through IS NULL OR b.target_month > s.actual_through)
  AND b.target_month BETWEEN MAKEDATE(s.fiscal_year, 1) + INTERVAL 3 MONTH AND MAKEDATE(s.fiscal_year + 1, 1) + INTERVAL 2 MONTH
UNION ALL
SELECT sa.scenario_id, sa.activity_id, sa.subject_id, NULL, sa.target_month, sa.amount,
       'actual', 'actual', FALSE, NULL
FROM scenario_actuals sa
JOIN scenarios s ON s.id = sa.scenario_id
WHERE s.is_locked AND sa.activity_id IS NOT NULL
UNION ALL
SELECT s.id, af.activity_id, af.subject_id, NULL, af.target_month, af.amount,
       'actual', 'actual', FALSE, NULL
FROM scenarios s
JOIN actual_facts af
  ON af.target_month BETWEEN MAKEDATE(s.fiscal_year, 1) + INTERVAL 3 MONTH AND s.actual_through
WHERE NOT s.is_locked AND s.actual_through IS NOT NULL AND af.activity_id IS NOT NULL;
