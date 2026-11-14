import { useEffect, useMemo, useState } from "react";
import { api } from "./api";
import type { Backtest, Inbox, Lane, LaneAction, LineRow, View } from "./types";
import { ColHead, Section, TableSrc, batchSource } from "./panels";
import { PRODUCT_LABEL, ea, slug } from "./format";
import { BASE, EChart, INK, INK2, LINE, RED, axis } from "./chart";

// 업무 레인 보드: 레인별 들어온 것·할 수 있는 일·하고 있는 일·막힌 것.
export function LanesView({ v, onAction }: { v: View; onAction: (a: LaneAction) => void }) {
  return (
    <Section title="업무 레인 보드" mode={["real", "mock"]} note={`업무일 ${v.state.scenario.sim_date} 기준(기록 ~${v.cutoff})입니다. "할 수 있는 일"을 누르면 해당 사건 입력 화면으로 이동합니다. 출처: 원본·계산·실제 작동(사건 루프)·모의(외부 회신).`}>
      <div className="lanes">
        {v.lanes.map((l) => (
          <LaneCard key={l.role} lane={l} onAction={onAction} />
        ))}
      </div>
    </Section>
  );
}

function LaneCard({ lane, onAction }: { lane: Lane; onAction: (a: LaneAction) => void }) {
  const items = (title: string, xs: { text: string; source: string }[] | null, cls = "") =>
    (xs ?? []).length > 0 && (
      <div className={`lane-box ${cls}`}>
        <div className="lane-title">{title}</div>
        <ul>
          {(xs ?? []).map((x, i) => (
            <li key={i}>
              {x.text} <span className="src">{x.source}</span>
            </li>
          ))}
        </ul>
      </div>
    );
  return (
    <div className="lane">
      <h3>{lane.role}</h3>
      {items("들어온 것", lane.incoming)}
      {(lane.can ?? []).length > 0 && (
        <div className="lane-box">
          <div className="lane-title">할 수 있는 일</div>
          <div className="lane-actions">
            {(lane.can ?? []).map((a) => (
              <button key={a.label} className={a.enabled ? "small" : "ghost small"} disabled={!a.enabled} title={a.reason ?? ""} onClick={() => onAction(a)}>
                {a.label}
              </button>
            ))}
          </div>
          {(lane.can ?? []).filter((a) => !a.enabled && a.reason).slice(0, 2).map((a) => (
            <div key={a.label} className="muted small">{a.label}: {a.reason}</div>
          ))}
        </div>
      )}
      {items("하고 있는 일", lane.doing)}
      {items("막힌 것", lane.blocked, "lane-blocked")}
      {!lane.incoming?.length && !lane.can?.length && !lane.doing?.length && !lane.blocked?.length && <p className="muted small">현재 처리할 업무가 없습니다.</p>}
    </div>
  );
}

// 접수함: 시계 날짜까지 최근 N일에 들어온 신청.
export function InboxView({ v }: { v: View }) {
  const [days, setDays] = useState(7);
  const [inbox, setInbox] = useState<Inbox | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [product, setProduct] = useState("");
  useEffect(() => {
    api.inbox(days).then(setInbox).catch((e: Error) => setErr(e.message));
  }, [days, v.state.scenario.sim_date, v.as_of, v.state.log?.length]);
  if (err) return <p className="error">{err}</p>;
  if (!inbox) return <p className="muted">불러오는 중…</p>;
  const orders = (inbox.orders ?? []).filter((o) => !product || o.product_id === product);
  return (
    <Section title="접수함" mode={["real"]} note={`업무일 ${inbox.sim_date} 기준(기록 ~${inbox.cutoff})입니다. 수주는 원본(sales_orders_cv)이며, 상태·추정 납기는 해당 일자까지의 기록으로 계산합니다.`}>
      <div className="kpis">
        <Tile label="기록 마지막 날 접수" value={`${ea(inbox.today)}건`} />
        <Tile label="최근 7일 접수" value={`${ea(inbox.last7)}건`} />
        <Tile label="최근 30일 접수" value={`${ea(inbox.last30)}건`} />
        <Tile label="최근 7일 출하" value={`${ea(inbox.shipped7)}건`} />
        <Tile label="진행 중 주문" value={`${ea(inbox.open_count)}건`} />
      </div>
      <div className="row">
        <label className="field inline">
          <span>기간</span>
          <select value={days} onChange={(e) => setDays(Number(e.target.value))}>
            <option value={1}>1일</option>
            <option value={7}>7일</option>
            <option value={30}>30일</option>
          </select>
        </label>
        <label className="field inline">
          <span>제품군</span>
          <select value={product} onChange={(e) => setProduct(e.target.value)}>
            <option value="">전체</option>
            {["PL-01", "PL-02", "PL-03", "PL-04", "PL-05", "PL-06"].map((p) => <option key={p}>{p}</option>)}
          </select>
        </label>
        <span className="muted small">
          {days}일 {ea(inbox.window_count)}건 · 팀별 {Object.entries(inbox.count_by_team).map(([k, n]) => `${k} ${n}`).join(" · ")}
          {inbox.truncated && " · 목록은 최근 300건까지"}
        </span>
      </div>
      {inbox.bid && (
        <p className="small">
          <b>입찰 {inbox.bid.bid_id}</b> 접수 {inbox.bid.received_date} · 상태 <span className={`pill pill-${slug(inbox.bid.state)}`}>{inbox.bid.state}</span> · 마감 {inbox.bid.deadline} <span className="src">모의(가상 도착)</span>
        </p>
      )}
      {(inbox.masil ?? []).length > 0 && (
        <p className="small">
          <b>Masil 발주</b> {(inbox.masil ?? []).map((m) => `${m.po_id} ${m.product_id} ${ea(m.qty)}EA(${m.order_date})`).join(" · ")} <span className="src">원본·합성 매핑</span>
        </p>
      )}
      <div className="scroll">
        <table className="grid">
          <thead>
            <tr><th>접수일</th><th className="c-left">주문</th><th className="c-left">고객</th><th className="c-left">팀</th><th className="c-left">제품군</th><th className="c-left">라인</th><th>수량</th><th>금액(백만)</th><th className="c-left">상태</th><th>추정 납기 / 출하일</th></tr>
          </thead>
          <tbody>
            {orders.map((o) => (
              <tr key={o.order_id}>
                <td>{o.order_date}</td><td className="c-left">{o.order_id}</td><td className="c-left">{o.customer_id === "NONE" ? o.agency_id : o.customer_id}</td><td className="c-left">{o.sales_team}</td>
                <td className="c-left">{o.product_id}</td><td className="c-left">{o.line_id}</td><td>{ea(o.qty)}</td><td>{ea(Math.round(o.amount_krw / 1e6))}</td>
                <td className="c-left"><span className={`pill pill-${slug(o.status)}`}>{o.status}</span></td>
                <td>{o.ship_date ?? o.est_due ?? "-"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Section>
  );
}

function Tile({ label, value }: { label: string; value: string }) {
  return (
    <div className="kpi kpi-ok">
      <div className="kpi-label">{label}</div>
      <div className="kpi-value">{value}</div>
    </div>
  );
}

// 생산 라인 보드: 라인이 무엇을 만들고(할 수 있는 작업), 지금 무엇을 하고, 얼마나 찼나.
export function LineBoardView({ v }: { v: View }) {
  const rows = v.line_board ?? [];
  const months = rows[0]?.load.map((m) => m.month) ?? [];
  const b = v.data_basis;
  return (
    <Section
      title="생산 라인 보드"
      mode={["real"]}
      note={
        <>
          업무일 {v.state.scenario.sim_date} 기준(기록 ~{v.cutoff})입니다. <b>이 보드의 월 생산·부하·상태는 배치 실적과 진행 주문 기록을 기준으로 합니다</b>({batchSource(v)}). 부하는 진행 주문의 추정 납기 달(A-14) + C-201 가예약이며, 대리점 주문 {ea(b.agency_orders)}건 · {ea(b.agency_qty)} EA는 포함되지 않습니다(A-17). 상태는 다음 달 부하 ÷ 배치 실적 월 생산으로 표시합니다(문턱 85%·100%, A-21, 표시용). <b>여유(연) 열만 엔진과 같은 능력 스냅샷</b>({b.snapshot_file} {b.snapshot_date})을 사용하므로 다른 열과 규모가 다릅니다. 아래 '원본 간 규모 차이'를 확인해 주세요.
        </>
      }
    >
      <LoadChart rows={rows} months={months} />
      <div className="scroll">
        <table className="grid">
          <thead>
            <tr>
              <th>라인</th><th className="c-left">할 수 있는 작업</th>
              <ColHead main="이번 달 배치" sub="배치 실적 · 완료/관행 횟수" />
              <ColHead main="최근 배치" sub="배치 실적 · 날짜 EA" />
              <ColHead main="보류(90일)" sub="배치 실적 · 건" />
              <ColHead main="월 생산" sub={`배치 실적 · EA/월 · ${b.t12_from}~${b.t12_to} 평균`} />
              <ColHead main="진행 주문" sub={`수주 기록 ~${b.cutoff} · 건 EA`} />
              {months.map((m) => <ColHead key={m} main={`${m} 부하`} sub="추정 납기 + 가예약 · EA/월 (÷ 월 생산)" />)}
              <ColHead main="여유" sub={`능력 스냅샷 ${b.snapshot_date} · EA/년`} />
              <ColHead main="Masil 보호" sub={`발주 ~${b.cutoff} · EA`} />
              <ColHead main="상태" sub="부하 ÷ 월 생산(A-21)" className="c-left" />
            </tr>
          </thead>
          <tbody>
            {rows.map((r: LineRow) => (
              <tr key={r.line_id} className={r.kind === "new" ? "row-new" : ""}>
                <td>{r.line_id} <span className="muted small">{r.plant_id}</span></td>
                <td className="c-left">{PRODUCT_LABEL[r.product_id] ?? r.product_id}{r.dedicated ? " · F1 전용" : ""}</td>
                <td>{r.kind === "new" ? "-" : `${r.batches_done}/${r.batches_typical}회`}</td>
                <td>{r.last_batch_date ? `${r.last_batch_date.slice(5)} ${ea(r.last_batch_qty)}` : "-"}</td>
                <td className={r.hold_batches_90d ? "bad" : ""}>{r.kind === "new" ? "-" : r.hold_batches_90d}</td>
                <td>{r.kind === "new" ? `월 능력 ${ea(r.monthly_cap)}` : ea(r.t12_monthly)}</td>
                <td>{r.kind === "new" ? "-" : `${ea(r.open_orders)}건 ${ea(r.open_qty)}`}</td>
                {r.load.map((m) => (
                  <td key={m.month}>
                    <LoadBar orders={m.orders} c201={m.c201} base={r.kind === "new" ? r.monthly_cap : r.t12_monthly} baseLabel={r.kind === "new" ? "OPT-02 월 능력(capex 추가 능력 ÷ 12)" : "배치 실적 월 생산"} />
                  </td>
                ))}
                <td>{r.kind === "new" ? "-" : ea(r.spare_annual)}{r.future_snapshot && r.kind !== "new" && <span className="src">미래 자료</span>}</td>
                <td>{r.masil_protected ? ea(r.masil_protected) : "-"}</td>
                <td className="c-left"><span className={`pill status-${slug(r.status)}`} title={r.note || undefined}>{r.status}</span>{r.note && <span className="note-mark" title={r.note}> 메모</span>}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <TableSrc>배치·보류·월 생산 = {batchSource(v)} · 진행 주문 = 수주 기록 ~{b.cutoff} · 부하 = 추정 납기 + C-201 가예약(EA/월, ÷ 월 생산) · 여유 = 능력 스냅샷 {b.snapshot_date}(EA/년) · Masil 보호 = 발주 ~{b.cutoff} · 상태 = 다음 달 부하 ÷ 월 생산(A-21)</TableSrc>
    </Section>
  );
}

function LoadBar({ orders, c201, base, baseLabel }: { orders: number; c201: number; base: number; baseLabel: string }) {
  const total = orders + c201;
  const pct = base > 0 ? Math.round((total / base) * 100) : 0;
  return (
    <span className={pct > 100 ? "bad" : ""} title={`주문 ${ea(orders)} + C-201 ${ea(c201)} / ${baseLabel} ${ea(base)} (EA/월)`}>
      {ea(total)} <span className="small">({pct}%)</span>
    </span>
  );
}

// 라인별 다음 달들 부하율(ECharts, 이슈 #63): 막대 = (주문 + C-201 가예약) ÷ 월 생산. 기준선 85%(주의)·100%(한도, 빨강).
// 달은 가까울수록 진하게(순서 있는 회색 단계), 100%를 넘는 막대는 빨강 테두리.
const MONTH_FILL = [INK, INK2, LINE];

function LoadChart({ rows, months }: { rows: LineRow[]; months: string[] }) {
  const option = useMemo(() => {
    const base = (r: LineRow) => (r.kind === "new" ? r.monthly_cap : r.t12_monthly);
    const pctOf = (r: LineRow, i: number) => {
      const m = r.load[i];
      const b = base(r);
      return m && b > 0 ? Math.round(((m.orders + m.c201) / b) * 100) : 0;
    };
    const series = months.map((month, i) => ({
      name: month,
      type: "bar",
      barMaxWidth: 22,
      barGap: "15%",
      itemStyle: { color: MONTH_FILL[i] ?? LINE, borderColor: i >= 2 ? INK2 : undefined, borderWidth: i >= 2 ? 1 : 0 },
      data: rows.map((r) => {
        const v = pctOf(r, i);
        return v > 100 ? { value: v, itemStyle: { borderColor: RED, borderWidth: 2 } } : v;
      }),
      ...(i === 0
        ? {
            markLine: {
              symbol: "none",
              silent: true,
              label: { position: "insideEndTop", color: INK2, fontSize: 11, formatter: "{c}%" },
              data: [
                { yAxis: 85, lineStyle: { color: INK2, type: "dashed", width: 1 } },
                { yAxis: 100, lineStyle: { color: RED, type: "dashed", width: 1.5 }, label: { color: RED } },
              ],
            },
          }
        : {}),
    }));
    return {
      ...BASE,
      grid: { left: 44, right: 40, top: 32, bottom: 28 },
      tooltip: {
        ...(BASE.tooltip as object),
        trigger: "axis",
        axisPointer: { type: "shadow", shadowStyle: { color: LINE } },
        formatter: (ps: { dataIndex: number }[]) => {
          const r = rows[ps[0]?.dataIndex ?? 0];
          if (!r) return "";
          const b = base(r);
          return [`<b>${r.line_id}</b> ${PRODUCT_LABEL[r.product_id] ?? r.product_id} · ${r.kind === "new" ? "OPT-02 월 능력" : "배치 실적 월 생산"} ${ea(b)} EA`,
            ...months.map((m, i) => {
              const x = r.load[i];
              const p = pctOf(r, i);
              return `${m}: 주문 ${ea(x?.orders ?? 0)} + C-201 ${ea(x?.c201 ?? 0)} → <b${p > 100 ? ` style="color:${RED}"` : ""}>${p}%</b>`;
            })].join("<br>");
        },
      },
      legend: { ...(BASE.legend as object), data: months },
      xAxis: axis({ type: "category", data: rows.map((r) => r.line_id), splitLine: { show: false } }),
      yAxis: axis({ type: "value", axisLine: { show: false }, axisLabel: { color: INK2, formatter: "{value}%" } }),
      series,
    };
  }, [rows, months]);
  if (!rows.length) return null;
  return <EChart option={option} height={260} label="라인별 월 부하율. 수치는 아래 표의 부하 열에서 확인할 수 있습니다." />;
}

// 정답 비교: 기준일에 추정한 납기(그날까지 기록만)와 그 뒤 실제 출하. 검증 전용.
export function BacktestView({ v }: { v: View }) {
  const [bt, setBt] = useState<Backtest | null>(null);
  useEffect(() => {
    api.backtest().then(setBt).catch(() => setBt(null));
  }, [v.state.scenario.sim_date, v.as_of]);
  if (!bt) return <p className="muted">불러오는 중…</p>;
  return (
    <Section title="정답 비교 (검증 전용)" mode={["real"]} note={bt.note}>
      {!bt.available ? (
        <p className="muted small">위 기준일 막대에서 2026-06-30보다 이른 날짜를 선택하면 비교할 수 있습니다.</p>
      ) : (
        <div className="cols">
          {["PL-01", "PL-02"].map((p) => (
            <div key={p}>
              <h3>{PRODUCT_LABEL[p]} · 진행 주문 {ea(bt.orders[p])}건 · 추정 달 적중 {(bt.hit_rate[p] ?? 0).toFixed(1)}%</h3>
              <table className="grid">
                <thead><tr><th>달</th><th>추정 납품(기준일)</th><th>실제 출하(기준일 뒤)</th><th>차이</th></tr></thead>
                <tbody>
                  {(bt.rows[p] ?? []).map((r) => (
                    <tr key={r.month}><td>{r.month}</td><td>{ea(r.estimated)}</td><td>{ea(r.actual)}</td><td className={Math.abs(r.actual - r.estimated) > 0 ? "bad" : ""}>{ea(r.actual - r.estimated)}</td></tr>
                  ))}
                </tbody>
              </table>
            </div>
          ))}
        </div>
      )}
    </Section>
  );
}
