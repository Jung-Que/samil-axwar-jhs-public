// Apache ECharts(이슈 #63): 쓰는 차트·컴포넌트만 가져온다(트리 셰이킹). chart.tsx가 처음 그래프를 그릴 때만
// 이 모듈을 따로 불러오므로(동적 import) 로그인·표 페이지의 번들에는 들어가지 않는다.
import * as echarts from "echarts/core";
import { BarChart, HeatmapChart, LineChart } from "echarts/charts";
import { GridComponent, LegendComponent, MarkLineComponent, TooltipComponent } from "echarts/components";
import { SVGRenderer } from "echarts/renderers";

echarts.use([BarChart, LineChart, HeatmapChart, GridComponent, TooltipComponent, LegendComponent, MarkLineComponent, SVGRenderer]);

export { echarts };
