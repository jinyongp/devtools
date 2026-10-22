<script setup>
const count = ref(0);
onMounted(() => { window.__compatReady = true; });
</script>
<template><main><h1 data-testid="marker">compat-before</h1><button data-testid="counter" @click="count++">{{ count }}</button></main></template>
