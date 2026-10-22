<script setup>
import { ref } from 'vue';
const count = ref(0);
</script>
<template><main><h1 data-testid="marker">compat-before</h1><button data-testid="counter" @click="count++">{{ count }}</button></main></template>
