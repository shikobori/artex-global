import * as React from "react";

const MOBILE_BREAKPOINT = 768;
// An expanded sidebar causes tables and cards to overlap at widths of 768-1279px.
// Below this threshold, automatically switch the sidebar to icon mode to leave room for content.
export const TABLET_BREAKPOINT = 1280;

function useBreakpoint(breakpoint: number) {
  const [below, setBelow] = React.useState<boolean | undefined>(undefined);

  React.useEffect(() => {
    const mql = window.matchMedia(`(max-width: ${breakpoint - 1}px)`);
    const onChange = () => setBelow(window.innerWidth < breakpoint);
    mql.addEventListener("change", onChange);
    setBelow(window.innerWidth < breakpoint);
    return () => mql.removeEventListener("change", onChange);
  }, [breakpoint]);

  return below;
}

export function useIsMobile() {
  return !!useBreakpoint(MOBILE_BREAKPOINT);
}

// Whether the viewport is tablet-width (768-1279px). Return undefined before the first measurement
// so callers can distinguish an unknown width from false.
export function useIsTablet() {
  return useBreakpoint(TABLET_BREAKPOINT);
}
