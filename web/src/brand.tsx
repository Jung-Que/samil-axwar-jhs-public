import type { ReactNode } from "react";

// 브랜드 층(이슈 #59): 백조 단색 일러스트. 이미지는 GPT(Codex CLI 이미지 생성)로 만들고 #111111 한 색·투명 바탕으로 바꿨다
// (web/public/brand/, 레퍼런스·규칙은 analysis/07_디자인_레퍼런스/01_서비스_레퍼런스.md).
// 규칙: 백조는 말하지 않는다(말풍선·1인칭 문구 없음, 뜻은 글자가 전한다). 처음(로그인)·비었을 때·막혔을 때·끝났을 때만 쓰고,
// 표·숫자 본문, 위급 경고·오류 띠, 되돌릴 수 없는 확인에는 넣지 않는다. 장식 그림이라 alt=""다.
// 크기 단계: 로고 24~32 · spot 72 · 빈/오류 144 · 로그인 히어로 480~600(같은 그림을 크기만 바꿔 쓴다).

export const SWAN = {
  hero: "/brand/01_login_hero.png", // 넓은 호수를 가로지른다
  done: "/brand/02_empty_done.png", // 목을 접고 쉰다: 할 일 없음
  waiting: "/brand/03_waiting.png", // 먼 곳을 본다: 회신·상신 대기
  success: "/brand/04_success.png", // 날개를 편다: 승인·제출 완료
  error: "/brand/05_error.png", // 뒤돌아보며 갸웃: 막힘·연결 실패
  notfound: "/brand/06_notfound.png", // 빈 호수에 깃털: 없는 페이지
  simulation: "/brand/07_simulation.png", // 파선 그림자: 시뮬레이션(모의 = 파선)
  mark: "/brand/08_mark.png", // 머리와 S자 목: 로고
} as const;

export type SwanName = keyof typeof SWAN;

export function Swan({ name, size, className }: { name: SwanName; size: number; className?: string }) {
  return <img className={`swan ${className ?? ""}`} src={SWAN[name]} alt="" width={size} height={size} draggable={false} />;
}

export function BrandMark({ size = 28 }: { size?: number }) {
  return <Swan name="mark" size={size} className="swan-mark" />;
}

// 빈 상태(Polaris·GitLab Pajamas): 그림 + 제목(짧게) + 한 줄 설명 + 주 버튼 하나.
// compact는 카드 안 가로 배치(spot 72px), 기본은 가운데 세로 배치(144px).
export function EmptyState({ swan, title, children, action, compact }: { swan: SwanName; title: string; children?: ReactNode; action?: ReactNode; compact?: boolean }) {
  return (
    <div className={compact ? "empty-state compact" : "empty-state"}>
      <Swan name={swan} size={compact ? 72 : 144} />
      <div className="empty-text">
        <p className="empty-title">{title}</p>
        {children && <p className="empty-desc muted small">{children}</p>}
        {action && <div className="row empty-action">{action}</div>}
      </div>
    </div>
  );
}
