<script>import { onMount } from 'svelte'; import { count } from '../counter.js'; onMount(() => { window.__compatReady = true; });</script>
<main><h1 data-testid="marker">compat-before</h1><button data-testid="counter" onclick={() => $count++}>{$count}</button><a href="/second">Second page</a><form method="POST"><button>Submit</button></form></main>
