<template>
  <el-tag :type="tagType" size="small" effect="light">{{ label }}</el-tag>
</template>

<script setup lang="ts">
import { computed } from 'vue';
import {
  RequirementStatusLabel,
  BidStatusLabel,
  ContractStatusLabel,
  DeliveryStatusLabel
} from '../../types/enums';

const props = defineProps<{ status: string; kind?: 'requirement' | 'bid' | 'contract' | 'delivery' }>();

const label = computed(() => {
  const map =
    props.kind === 'bid'
      ? BidStatusLabel
      : props.kind === 'contract'
        ? ContractStatusLabel
        : props.kind === 'delivery'
          ? DeliveryStatusLabel
          : RequirementStatusLabel;
  return map[props.status] || props.status;
});

const tagType = computed(() => {
  switch (props.status) {
    case 'open':
    case 'pending':
    case 'pending_signature':
    case 'pending_review':
    case 'bidding':
    case 'submitted':
      return 'warning';
    case 'in_progress':
    case 'accepted':
    case 'review':
      return props.kind === 'delivery' ? 'success' : 'primary';
    case 'completed':
    case 'done':
      return 'success';
    case 'cancelled':
    case 'rejected':
    case 'withdrawn':
    case 'terminated':
      return 'danger';
    default:
      return 'info';
  }
});
</script>
