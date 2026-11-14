import { useEffect, useRef } from "react";
import type { ECharts, EChartsCoreOption } from "echarts/core";

// 그래프는 직접 그리지 않고 Apache ECharts로 그린다(이슈 #63, 사용자 지시 11/13).
// 팔레트는 다섯 색(#FFFFFF #F2F2F2 #D7263D #4F4F4F #111111)뿐이라 계열은 채움·무늬(decal)·선 모양으로 구분하고,
// 빨강은 부족·한도 초과 같은 경고에만 쓴다. 정확한 값은 차트 옆 표가 맡는다(색만으로 뜻을 전하지 않는다).

export const INK = "#111111";
export const INK2 = "#4F4F4F";
export const LINE = "#F2F2F2";
export const PAPER = "#FFFFFF";
export const RED = "#D7263D";

// 모든 차트가 같이 쓰는 바탕: 글꼴·글자색·옅은 축·툴팁
export const BASE: EChartsCoreOption = {
  animationDuration: 300,
  textStyle: { fontFamily: 'system-ui, -apple-system, "Segoe UI", "Malgun Gothic", "Apple SD Gothic Neo", sans-serif', color: INK2, fontSize: 12 },
  tooltip: { backgroundColor: PAPER, borderColor: INK2, borderWidth: 1, textStyle: { color: INK, fontSize: 12 }, extraCssText: "box-shadow:none;border-radius:6px;" },
  legend: { top: 0, left: 0, itemWidth: 14, itemHeight: 10, textStyle: { color: INK, fontSize: 12 } },
};

export const axis = (o: Record<string, unknown> = {}) => ({
  axisLine: { lineStyle: { color: INK2 } },
  axisTick: { show: false },
  axisLabel: { color: INK2 },
  splitLine: { lineStyle: { color: LINE } },
  ...o,
});

let loader: Promise<typeof import("./echartsCore")> | null = null;

type Props = {
  option: EChartsCoreOption;
  height: number;
  label: string; // 스크린 리더용 차트 이름(정확한 값은 옆 표)
  onClick?: (p: { seriesIndex?: number; dataIndex?: number; data?: unknown }) => void;
};

// EChart는 ECharts 인스턴스 하나를 div에 붙인다. 처음 그릴 때 echartsCore를 불러오고, 크기가 바뀌면 다시 맞춘다.
export function EChart({ option, height, label, onClick }: Props) {
  const ref = useRef<HTMLDivElement>(null);
  const inst = useRef<ECharts | null>(null);
  const latest = useRef(option);
  const click = useRef(onClick);
  latest.current = option;
  click.current = onClick;

  useEffect(() => {
    let alive = true;
    let ro: ResizeObserver | null = null;
    (loader ??= import("./echartsCore")).then(({ echarts }) => {
      if (!alive || !ref.current) return;
      const c = echarts.init(ref.current, undefined, { renderer: "svg" });
      inst.current = c;
      c.setOption(latest.current, true);
      c.on("click", (p) => click.current?.(p as { seriesIndex?: number; dataIndex?: number; data?: unknown }));
      ro = new ResizeObserver(() => c.resize());
      ro.observe(ref.current);
    });
    return () => {
      alive = false;
      ro?.disconnect();
      inst.current?.dispose();
      inst.current = null;
    };
  }, []);

  useEffect(() => {
    inst.current?.setOption(option, true);
  }, [option]);

  return <div ref={ref} className="echart" style={{ height }} role="img" aria-label={label} />;
}
