'use client';
import { useEffect, useState } from 'react';
export default function Page() {
  const [count, setCount] = useState(0);
  useEffect(() => { window.__compatReady = true; }, []);
  return <main><h1 data-testid="marker">compat-before</h1><button data-testid="counter" onClick={() => setCount(count + 1)}>{count}</button></main>;
}
