import { Fragment, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { api } from "./api";
import type { Backtest, Department, Inbox, Lane, LaneAction, LaneItem, LineRow, View } from "./types";
import { ColHead, Section, TableSrc, batchSource } from "./panels";
import { PRODUCT_LABEL, ea, slug } from "./format";
import { BASE, EChart, INK, INK2, LINE, RED, axis } from "./chart";
import { EmptyState } from "./brand";
import { Link } from "./router";
import { pathOf } from "./frame";

// 부서 계정 → 레인 이름(서버 boards.go laneOf와 같은 규칙: 영업 임원 → 영업, 그룹 재무 → 재무·전략).
function laneOfDept(d: Department): string {
  if (d.id === "영업 임원") return "영업";
  if (d.id === "그룹 재무") return "재무·전략";
  return d.id;
}

// 업무 레인 보드: 레인별 들어온 것·할 수 있는 일·하고 있는 일·막힌 것.
// 밀집도(이슈 #71): 소속 부서 레인만 펼치고 나머지는 이름·개수 한 줄로 접는다. dept가 없으면 모두 펼친다.
// 소속 레인이 없는 계정(운영 관리)은 모두 접힌 개요로 시작한다.
export function LanesView({ v, onAction, dept }: { v: View; onAction: (a: LaneAction) => void; dept?: Department }) {
  const roles = v.lanes.map((l) => l.role);
  const mine = dept ? laneOfDept(dept) : null;
  const hasMine = mine !== null && roles.includes(mine);
  const initial = () => new Set(mine === null ? roles : roles.filter((r) => r === mine));
  const [open, setOpen] = useState<Set<string>>(initial);
  useEffect(() => setOpen(initial()), [mine]); // eslint-disable-line react-hooks/exhaustive-deps
  const toggle = (r: string) =>
    setOpen((prev) => {
      const n = new Set(prev);
      if (n.has(r)) n.delete(r);
      else n.add(r);
      return n;
    });
  const allOpen = roles.every((r) => open.has(r));
  const hint = mine === null ? "" : hasMine ? "소속 부서 레인을 펼쳐 표시합니다. 다른 레인은 이름을 누르면 펼쳐집니다. " : "레인 이름을 누르면 펼쳐집니다. ";
  return (
    <Section title="업무 레인 보드" mode={["real", "mock"]} note={`업무일 ${v.state.scenario.sim_date} 기준(기록 ~${v.cutoff})입니다. ${hint}"할 수 있는 일"을 누르면 해당 사건 입력 화면으로 이동합니다. 출처: 원본·계산·실제 작동(사건 루프)·모의(외부 회신).`}>
      <div className="row lane-tools">
        <button type="button" className="ghost small" onClick={() => setOpen(new Set(allOpen ? [] : roles))}>{allOpen ? "모두 접기" : "모두 펼치기"}</button>
      </div>
      <div className="lane-stack">
        {v.lanes.map((l) => (
          <LaneCard key={l.role} lane={l} open={open.has(l.role)} mine={l.role === mine} onToggle={() => toggle(l.role)} onAction={onAction} />
        ))}
      </div>
    </Section>
  );
}

// Masil SCM 레인의 발주 줄("SPO-… PL-02 1,000 EA → 2026-07-20 입고 필요(보호 약속)")은 한 줄 요약 + 펼침으로 묶는다.
const SPO_LINE = /^(\S+) (PL-\d+) ([\d,]+) EA → (\S+) 입고 필요/;

function LaneItems({ xs, groupSpo }: { xs: LaneItem[]; groupSpo: boolean }) {
  const spo = groupSpo ? xs.filter((x) => SPO_LINE.test(x.text)) : [];
  const grouped = spo.length >= 2;
  const rest = grouped ? xs.filter((x) => !spo.includes(x)) : xs;
  const total: Record<string, number> = {};
  for (const x of grouped ? spo : []) {
    const m = SPO_LINE.exec(x.text);
    if (m) total[m[2]] = (total[m[2]] ?? 0) + Number(m[3].replace(/,/g, ""));
  }
  const order = ["PL-02", "PL-01", ...Object.keys(total).filter((p) => p !== "PL-02" && p !== "PL-01").sort()];
  const sums = order.filter((p) => total[p]).map((p) => `${p} ${ea(total[p])} EA`);
  return (
    <ul>
      {grouped && (
        <li className="lane-group">
          <details>
            <summary>
              Masil 발주 {spo.length}건 · {sums.join(" · ")} 입고 필요(보호 약속) <span className="src">{spo[0].source}</span>
            </summary>
            <ul>
              {spo.map((x, i) => <li key={i} className="small">{x.text}</li>)}
            </ul>
          </details>
        </li>
      )}
      {rest.map((x, i) => <LaneLine key={i} item={x} />)}
    </ul>
  );
}

// 레인 항목 한 줄: 두 줄까지 보이고, 넘치면 글을 눌러 펼치고 접는다(전체 글과 출처는 툴팁에도).
function LaneLine({ item }: { item: LaneItem }) {
  const ref = useRef<HTMLSpanElement>(null);
  const [open, setOpen] = useState(false);
  const [over, setOver] = useState(false);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el || open) return;
    const check = () => setOver(el.scrollHeight > el.clientHeight + 1);
    check();
    const ro = new ResizeObserver(check);
    ro.observe(el);
    return () => ro.disconnect();
  }, [item.text, open]);
  const more = over || open;
  const flip = () => setOpen(!open);
  return (
    <li title={open ? undefined : `${item.text} (${item.source})`}>
      <span
        ref={ref}
        className={`lane-text${open ? "" : " clamp-2"}${more ? " is-more" : ""}`}
        role={more ? "button" : undefined}
        tabIndex={more ? 0 : undefined}
        aria-expanded={more ? open : undefined}
        onClick={more ? flip : undefined}
        onKeyDown={more ? (e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); flip(); } } : undefined}
      >
        {item.text} <span className="src">{item.source}</span>
      </span>
    </li>
  );
}

function LaneCard({ lane, open, mine, onToggle, onAction }: { lane: Lane; open: boolean; mine: boolean; onToggle: () => void; onAction: (a: LaneAction) => void }) {
  const incoming = lane.incoming ?? [];
  const can = lane.can ?? [];
  const doing = lane.doing ?? [];
  const blocked = lane.blocked ?? [];
  const canOn = can.filter((a) => a.enabled).length;
  const empty = !incoming.length && !can.length && !doing.length && !blocked.length;
  const box = (title: string, xs: LaneItem[], cls = "") =>
    xs.length > 0 && (
      <div className={`lane-box ${cls}`}>
        <div className="lane-title">{title}</div>
        <LaneItems xs={xs} groupSpo={lane.role === "Masil SCM"} />
      </div>
    );
  return (
    <div className={`lane lane-fold ${open ? "is-open" : ""} ${mine ? "is-mine" : ""}`}>
      <h3 className="lane-h">
        <button type="button" className="lane-head" aria-expanded={open} onClick={onToggle}>
          <span className="lane-caret" aria-hidden="true">{open ? "▾" : "▸"}</span>
          <span className="lane-name">{lane.role}</span>
          {mine && <span className="lane-mine">소속 부서</span>}
          <span className="lane-counts">
            들어온 것 {incoming.length} · 할 수 있는 일 {canOn} · <span className={blocked.length ? "bad" : ""}>막힌 것 {blocked.length}</span>
          </span>
        </button>
      </h3>
      {open && (
        <div className="lane-body">
          {box("들어온 것", incoming)}
          {can.length > 0 && (
            <div className="lane-box">
              <div className="lane-title">할 수 있는 일</div>
              <div className="lane-actions">
                {/* 비활성 사유는 버튼 툴팁(title)에만 둔다 */}
                {can.map((a) => (
                  <button key={a.label} className={a.enabled ? "small" : "ghost small"} disabled={!a.enabled} title={a.reason ?? ""} onClick={() => onAction(a)}>
                    {a.label}
                  </button>
                ))}
              </div>
            </div>
          )}
          {box("하고 있는 일", doing)}
          {box("막힌 것", blocked, "lane-blocked")}
          {empty && <p className="muted small">현재 처리할 업무가 없습니다.</p>}
        </div>
      )}
    </div>
  );
}

const INBOX_FIRST = 20;

// 접수함: 시계 날짜까지 최근 N일에 들어온 신청.
export function InboxView({ v }: { v: View }) {
  const [days, setDays] = useState(7);
  const [inbox, setInbox] = useState<Inbox | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [product, setProduct] = useState("");
  const [showAll, setShowAll] = useState(false);
  useEffect(() => {
    api.inbox(days).then(setInbox).catch((e: Error) => setErr(e.message));
  }, [days, v.state.scenario.sim_date, v.as_of, v.state.log?.length]);
  useEffect(() => setShowAll(false), [days, product]);
  if (err) return <p className="error">{err}</p>;
  if (!inbox) return <p className="muted">불러오는 중…</p>;
  const orders = (inbox.orders ?? []).filter((o) => !product || o.product_id === product);
  // 밀집도(이슈 #71): 처음 20건만 보이고 나머지는 "더 보기"로 펼친다.
  const shown = showAll ? orders : orders.slice(0, INBOX_FIRST);
  const rest = orders.length - shown.length;
  const masil = inbox.masil ?? [];
  const masilQty = masil.reduce((n, m) => n + m.qty, 0);
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
      {masil.length > 0 && (
        <details className="fold-line small">
          <summary>
            <b>Masil 발주</b> {masil.length}건 · 합계 {ea(masilQty)} EA <span className="src">원본·합성 매핑</span>
          </summary>
          <table className="grid grid-auto">
            <thead>
              <tr><th className="c-fit">발주</th><th className="c-fit c-left">제품군</th><th className="c-num">수량(EA)</th><th className="c-fit c-left">발주일</th></tr>
            </thead>
            <tbody>
              {masil.map((m) => (
                <tr key={m.po_id}><td className="c-fit">{m.po_id}</td><td className="c-fit c-left">{m.product_id}</td><td className="c-num">{ea(m.qty)}</td><td className="c-fit c-left">{m.order_date}</td></tr>
              ))}
            </tbody>
          </table>
        </details>
      )}
      <div className="scroll">
        <table className="grid">
          <thead>
            <tr>
              <th className="c-fit">접수일</th><th className="c-fit c-left">주문</th><th className="c-fit c-left">고객</th><th className="c-fit c-left">팀</th><th className="c-fit c-left">제품군</th><th className="c-fit c-left">라인</th>
              <th className="c-num">수량</th><th className="c-num">금액(백만)</th><th className="c-fit c-left">상태</th><ColHead main="납기 / 출하일" sub="진행: 추정 납기(기록 기준 계산) · 출하 완료: 출하일" className="c-fit c-left" />
            </tr>
          </thead>
          <tbody>
            {shown.map((o) => (
              <tr key={o.order_id}>
                <td className="c-fit">{o.order_date}</td><td className="c-fit c-left">{o.order_id}</td><td className="c-fit c-left">{o.customer_id === "NONE" ? o.agency_id : o.customer_id}</td><td className="c-fit c-left">{o.sales_team}</td>
                <td className="c-fit c-left">{o.product_id}</td><td className="c-fit c-left">{o.line_id}</td><td className="c-num">{ea(o.qty)}</td><td className="c-num">{ea(Math.round(o.amount_krw / 1e6))}</td>
                {/* 흔한 상태(진행)는 글자로, 그 밖의 상태만 배지로 */}
                <td className="c-fit c-left">{o.status === "진행" ? o.status : <span className={`pill pill-${slug(o.status)}`}>{o.status}</span>}</td>
                <td className="c-fit c-left">{o.ship_date ?? o.est_due ?? "-"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {(rest > 0 || (showAll && orders.length > INBOX_FIRST)) && (
        <div className="row more-row">
          {rest > 0 ? (
            <button type="button" className="ghost small" onClick={() => setShowAll(true)}>더 보기({ea(rest)}건 남음)</button>
          ) : (
            <button type="button" className="ghost small" onClick={() => setShowAll(false)}>처음 {INBOX_FIRST}건만 보기</button>
          )}
        </div>
      )}
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
// 밀집도(이슈 #71): 열 순서 라인·작업·상태·부하·월 생산·진행 주문·여유. 배치·보류·Masil 보호는 라인별 펼침 줄로,
// 모든 라인의 부하가 0인 달은 열을 숨기고 표 아래 출처 줄에 적는다.
const loadOf = (r: LineRow, month: string) => r.load.find((x) => x.month === month);

export function LineBoardView({ v }: { v: View }) {
  const rows = v.line_board ?? [];
  const allMonths = rows[0]?.load.map((m) => m.month) ?? [];
  const zero = allMonths.filter((m) =>
    rows.every((r) => {
      const x = loadOf(r, m);
      return !x || x.orders + x.c201 === 0;
    }),
  );
  const months = allMonths.filter((m) => !zero.includes(m));
  const b = v.data_basis;
  const future = rows.some((r) => r.future_snapshot && r.kind !== "new");
  const [open, setOpen] = useState<Set<string>>(() => new Set());
  const toggle = (id: string) =>
    setOpen((prev) => {
      const n = new Set(prev);
      if (n.has(id)) n.delete(id);
      else n.add(id);
      return n;
    });
  const cols = 6 + months.length;
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
        <table className="grid line-board">
          <thead>
            <tr>
              <th className="c-fit">라인</th>
              <th className="c-left lb-wrap">할 수 있는 작업</th>
              <ColHead main="상태" sub="다음 달 부하 ÷ 월 생산(A-21) · 문턱 85%·100%" className="c-fit c-left" />
              {months.map((m) => (
                <ColHead key={m} main={`${m} 부하`} sub="추정 납기 + 가예약 · EA/월 (÷ 월 생산)" className="c-num" />
              ))}
              <ColHead main="월 생산" sub={`배치 실적 · EA/월 · ${b.t12_from}~${b.t12_to} 평균`} className="c-num" />
              <ColHead main="진행 주문" sub={`수주 기록 ~${b.cutoff} · 건 EA`} className="c-num" />
              <ColHead main="여유" sub={`능력 스냅샷 ${b.snapshot_date} · EA/년${future ? " · 미래 자료" : ""}`} className="c-num" />
            </tr>
          </thead>
          <tbody>
            {rows.map((r: LineRow) => {
              const nw = r.kind === "new";
              const isOpen = open.has(r.line_id);
              return (
                <Fragment key={r.line_id}>
                  <tr className={nw ? "row-new" : ""}>
                    <td className="c-fit">
                      <button
                        type="button"
                        className="row-toggle"
                        aria-expanded={isOpen}
                        aria-label={`${r.line_id} 배치·보류·Masil 보호 ${isOpen ? "접기" : "펼치기"}`}
                        title={isOpen ? "배치·보류·Masil 보호 접기" : "배치·보류·Masil 보호 펼치기"}
                        onClick={() => toggle(r.line_id)}
                      >
                        {isOpen ? "▾" : "▸"}
                      </button>
                      {r.line_id} <span className="muted small">{r.plant_id}</span>
                    </td>
                    {/* 좁은 폭에서는 작업·상태·월 생산·진행 주문 칸만 두 줄로 접힌다(덩어리는 nowrap) */}
                    <td className="c-left lb-wrap">
                      <span className="nowrap">{PRODUCT_LABEL[r.product_id] ?? r.product_id}</span>
                      {r.dedicated && <> <span className="nowrap">· F1 전용</span></>}
                    </td>
                    <td className="c-left lb-wrap">
                      <span className={`pill status-${slug(r.status)}`} title={r.note || undefined}>{r.status}</span>
                      {r.note && <span className="note-mark" title={r.note}> 메모</span>}
                    </td>
                    {months.map((m) => {
                      const x = loadOf(r, m);
                      return (
                        <td key={m} className="c-num">
                          <LoadBar orders={x?.orders ?? 0} c201={x?.c201 ?? 0} base={nw ? r.monthly_cap : r.t12_monthly} baseLabel={nw ? "OPT-02 월 능력(capex 추가 능력 ÷ 12)" : "배치 실적 월 생산"} />
                        </td>
                      );
                    })}
                    <td className="c-num lb-wrap">{nw ? <><span className="nowrap">월 능력</span> {ea(r.monthly_cap)}</> : ea(r.t12_monthly)}</td>
                    <td className="c-num lb-wrap">{nw ? "-" : `${ea(r.open_orders)}건 ${ea(r.open_qty)}`}</td>
                    <td className="c-num">{nw ? "-" : ea(r.spare_annual)}</td>
                  </tr>
                  {isOpen && (
                    <tr className="line-detail">
                      <td colSpan={cols}>
                        <dl className="kv-line">
                          <div><dt title="배치 실적 · 이번 달 완료 / 관행 횟수">이번 달 배치</dt><dd>{nw ? "-" : `${r.batches_done}/${r.batches_typical}회`}</dd></div>
                          <div><dt title="배치 실적 · 마지막 배치 날짜와 수량">최근 배치</dt><dd>{r.last_batch_date ? `${r.last_batch_date} · ${ea(r.last_batch_qty)} EA` : "-"}</dd></div>
                          <div><dt title="배치 실적 · 최근 90일 품질 판정 합격 외">보류(90일)</dt><dd className={r.hold_batches_90d ? "bad" : ""}>{nw ? "-" : `${r.hold_batches_90d}건`}</dd></div>
                          <div><dt title={`Masil 발주 ~${b.cutoff} · EA`}>Masil 보호</dt><dd>{r.masil_protected ? `${ea(r.masil_protected)} EA` : "-"}</dd></div>
                          {r.note && <div><dt>메모</dt><dd>{r.note}</dd></div>}
                        </dl>
                      </td>
                    </tr>
                  )}
                </Fragment>
              );
            })}
          </tbody>
        </table>
      </div>
      <TableSrc>
        부하 = 추정 납기 + C-201 가예약(EA/월, ÷ 월 생산) · 월 생산·배치·보류 = {batchSource(v)} · 진행 주문 = 수주 기록 ~{b.cutoff} · 여유 = 능력 스냅샷 {b.snapshot_date}(EA/년{future ? ", 미래 자료" : ""}) · Masil 보호 = 발주 ~{b.cutoff} · 상태 = 다음 달 부하 ÷ 월 생산(A-21) · 배치·보류·Masil 보호는 라인의 ▸에서 확인
        {zero.length > 0 && ` · ${zero.join("·")} 부하 = 모든 라인 0(열 생략)`}
      </TableSrc>
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
// 달은 가까울수록 진하게(순서 있는 회색 단계), 100%를 넘는 막대는 빨강 테두리. 표에서 숨긴 달(모든 라인 0)은 그리지 않는다.
const MONTH_FILL = [INK, INK2, LINE];

function LoadChart({ rows, months }: { rows: LineRow[]; months: string[] }) {
  const option = useMemo(() => {
    const base = (r: LineRow) => (r.kind === "new" ? r.monthly_cap : r.t12_monthly);
    const pctOf = (r: LineRow, month: string) => {
      const m = loadOf(r, month);
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
        const v = pctOf(r, month);
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
            ...months.map((m) => {
              const x = loadOf(r, m);
              const p = pctOf(r, m);
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
  if (!bt.available) {
    // 밀집도(이슈 #71): 안내문 두 줄 대신 빈 상태 하나. 기준일이 이른 세션은 시뮬레이션 페이지에서 생성한다.
    return (
      <Section title="정답 비교 (검증 전용)" mode={["real"]}>
        <EmptyState
          compact
          swan="simulation"
          title="현재 비교할 실제 출하가 없습니다"
          action={<Link to={pathOf("sim")} className="btn ghost-btn small-btn">시뮬레이션에서 세션 생성</Link>}
        >
          현재 세션은 기록 마지막 날({bt.cutoff}) 기준이라 이후 출하 기록이 없습니다. 기준일이 2026-06-30보다 이른 세션에서 추정 납기와 실제 출하를 비교할 수 있습니다.
        </EmptyState>
      </Section>
    );
  }
  return (
    <Section title="정답 비교 (검증 전용)" mode={["real"]} note={bt.note}>
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
    </Section>
  );
}
