import { useState } from 'react';
export default function App() {
  const [count, setCount] = useState(0);
  return <main><h1 data-testid="marker">compat-before</h1><button data-testid="counter" onClick={() => setCount(count + 1)}>{count}</button></main>;
}
