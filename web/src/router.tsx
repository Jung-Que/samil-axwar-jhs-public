import { useEffect, useState, type AnchorHTMLAttributes, type MouseEvent } from "react";

// 화면 주소(이슈 #53). SPA 그대로 브라우저 history API로 페이지를 바꾼다(의존성 추가 없음).
// 서버 spaHandler가 확장자 없는 주소를 index.html로 돌리므로 어느 주소에서 새로고침해도 같은 페이지가 열린다.

const EVT = "w05:navigate";

export type Loc = { path: string; query: URLSearchParams };

function read(): Loc {
  return { path: window.location.pathname.replace(/\/+$/, "") || "/", query: new URLSearchParams(window.location.search) };
}

// navigate는 주소를 바꾸고 화면에 알린다. replace면 뒤로 가기 기록을 남기지 않는다.
export function navigate(to: string, replace = false) {
  if (to === window.location.pathname + window.location.search) return;
  if (replace) window.history.replaceState(null, "", to);
  else window.history.pushState(null, "", to);
  window.dispatchEvent(new Event(EVT));
  if (!replace) window.scrollTo(0, 0);
}

// useLocation은 지금 주소다(뒤로·앞으로 가기와 navigate에 맞춰 바뀐다).
export function useLocation(): Loc {
  const [loc, setLoc] = useState(read);
  useEffect(() => {
    const on = () => setLoc(read());
    window.addEventListener("popstate", on);
    window.addEventListener(EVT, on);
    return () => {
      window.removeEventListener("popstate", on);
      window.removeEventListener(EVT, on);
    };
  }, []);
  return loc;
}

// Link는 새로고침 없이 페이지를 바꾸는 링크다. 새 탭 열기(Ctrl·가운데 클릭)는 브라우저에 맡긴다.
export function Link({ to, onClick, ...rest }: { to: string } & AnchorHTMLAttributes<HTMLAnchorElement>) {
  const click = (e: MouseEvent<HTMLAnchorElement>) => {
    onClick?.(e);
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    e.preventDefault();
    navigate(to);
  };
  return <a href={to} onClick={click} {...rest} />;
}
