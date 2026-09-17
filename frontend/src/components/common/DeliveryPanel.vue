<template>
  <el-card class="delivery-panel">
    <template #header>
      <div class="panel-head">
        <b>交付验收</b>
        <StatusBadge v-if="delivery" :status="delivery.status" kind="delivery" />
      </div>
    </template>

    <el-empty v-if="!delivery && canSubmit" description="尚未提交交付" :image-size="60" />
    <el-empty v-else-if="!delivery" description="乙方尚未提交交付" :image-size="60" />

    <template v-if="delivery">
      <div class="delivery-meta muted">
        <span>第 {{ delivery.revision }} 次交付</span>
        <span>提交人：{{ delivery.submitter?.name || '乙方' }}</span>
        <span>提交时间：{{ formatDateTime(delivery.submittedAt) }}</span>
      </div>
      <p class="delivery-desc">{{ delivery.description }}</p>
      <div v-if="delivery.attachments.length" class="delivery-files">
        <span class="muted">附件：</span>
        <el-link
          v-for="(file, index) in delivery.attachments"
          :key="index"
          :href="file"
          target="_blank"
          type="primary"
          class="file-link"
        >
          <el-icon><Paperclip /></el-icon>
          {{ attachmentName(file, index) }}
        </el-link>
      </div>
      <el-alert
        v-if="delivery.status === 'rejected' && delivery.rejectReason"
        class="reject-alert"
        type="error"
        :closable="false"
        show-icon
      >
        <template #title>驳回原因</template>
        {{ delivery.rejectReason }}
      </el-alert>
      <div v-if="delivery.reviewedAt" class="delivery-meta muted">
        <span>验收人：{{ delivery.reviewer?.name || '甲方' }}</span>
        <span>验收时间：{{ formatDateTime(delivery.reviewedAt) }}</span>
      </div>
    </template>

    <!-- 乙方：执行中可提交（被驳回后可重新提交） -->
    <div v-if="canSubmit" class="panel-actions">
      <el-button type="primary" @click="openSubmit">
        {{ delivery ? '重新提交交付' : '提交交付' }}
      </el-button>
    </div>

    <!-- 甲方：待验收可驳回/接受，按钮在处理中禁用以防重复提交 -->
    <div v-if="canReview" class="panel-actions">
      <el-button type="danger" :loading="reviewing" @click="openReject">驳回</el-button>
      <el-button type="success" :loading="reviewing" @click="accept">接受并完成合同</el-button>
    </div>

    <el-dialog v-model="submitVisible" title="提交交付说明" width="520px">
      <el-form :model="form" label-width="80px">
        <el-form-item label="交付说明" required>
          <el-input
            v-model="form.description"
            type="textarea"
            :rows="4"
            maxlength="2000"
            show-word-limit
            placeholder="说明本次交付的内容、完成情况与验收方式（至少 5 个字）"
          />
        </el-form-item>
        <el-form-item label="附件">
          <el-input
            v-model="attachmentText"
            type="textarea"
            :rows="2"
            placeholder="附件链接，多个链接用换行或逗号分隔（可选）"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="submitVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submit">确认提交</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="rejectVisible" title="驳回交付" width="480px">
      <el-form label-width="80px">
        <el-form-item label="驳回原因" required>
          <el-input
            v-model="rejectReason"
            type="textarea"
            :rows="4"
            maxlength="500"
            show-word-limit
            placeholder="请填写驳回原因，乙方修改后可重新提交"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="rejectVisible = false">取消</el-button>
        <el-button type="danger" :loading="reviewing" @click="reject">确认驳回</el-button>
      </template>
    </el-dialog>
  </el-card>
</template>

<script setup lang="ts">
import { computed, reactive, ref } from 'vue';
import { ElMessage, ElMessageBox } from 'element-plus';
import { Paperclip } from '@element-plus/icons-vue';
import type { ContractDelivery } from '../../types';
import { contractApi } from '../../api/contract';
import StatusBadge from './StatusBadge.vue';
import { formatDate } from '../../utils/formatCurrency';

const props = defineProps<{
  contractId: number;
  contractStatus: string;
  delivery: ContractDelivery | null;
  isPartyA: boolean;
  isPartyB: boolean;
}>();

const emit = defineEmits<{ (e: 'changed'): void }>();

const submitting = ref(false);
const reviewing = ref(false);
const submitVisible = ref(false);
const rejectVisible = ref(false);
const attachmentText = ref('');
const rejectReason = ref('');
const form = reactive({ description: '' });

// 只有执行中的合同、且由乙方提交；待验收/已完成期间表单入口不出现。
const canSubmit = computed(
  () => props.isPartyB && props.contractStatus === 'in_progress'
);
// 只有待验收状态、且由甲方处理；杜绝重复接受/驳回。
const canReview = computed(
  () =>
    props.isPartyA &&
    props.contractStatus === 'pending_review' &&
    props.delivery?.status === 'submitted'
);

function formatDateTime(value?: string): string {
  if (!value) return '-';
  const date = formatDate(value);
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return date;
  const time = `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
  return `${date} ${time}`;
}

function attachmentName(file: string, index: number): string {
  const segments = file.split('/');
  const name = segments[segments.length - 1];
  return name || `附件 ${index + 1}`;
}

function parseAttachments(): string[] {
  return attachmentText.value
    .split(/[\n,，]/)
    .map((item) => item.trim())
    .filter(Boolean);
}

function openSubmit() {
  form.description = '';
  attachmentText.value = '';
  submitVisible.value = true;
}

async function submit() {
  if (form.description.trim().length < 5) {
    ElMessage.warning('请填写至少 5 个字的交付说明');
    return;
  }
  submitting.value = true;
  try {
    await contractApi.submitDelivery(props.contractId, {
      description: form.description.trim(),
      attachments: parseAttachments()
    });
    ElMessage.success('交付已提交，等待甲方验收');
    submitVisible.value = false;
    emit('changed');
  } finally {
    submitting.value = false;
  }
}

function openReject() {
  rejectReason.value = '';
  rejectVisible.value = true;
}

async function reject() {
  if (rejectReason.value.trim().length === 0) {
    ElMessage.warning('驳回时必须填写驳回原因');
    return;
  }
  reviewing.value = true;
  try {
    await contractApi.reviewDelivery(props.contractId, {
      action: 'reject',
      rejectReason: rejectReason.value.trim()
    });
    ElMessage.success('已驳回，合同回到执行中');
    rejectVisible.value = false;
    emit('changed');
  } finally {
    reviewing.value = false;
  }
}

async function accept() {
  try {
    await ElMessageBox.confirm('接受后合同即完成，确认验收通过？', '确认接受', {
      type: 'success',
      confirmButtonText: '接受',
      cancelButtonText: '取消'
    });
  } catch {
    return;
  }
  reviewing.value = true;
  try {
    await contractApi.reviewDelivery(props.contractId, { action: 'accept' });
    ElMessage.success('验收通过，合同已完成');
    emit('changed');
  } finally {
    reviewing.value = false;
  }
}
</script>

<style scoped>
.delivery-panel { margin-bottom: 24px; }
.panel-head { display: flex; justify-content: space-between; align-items: center; }
.delivery-meta { display: flex; flex-wrap: wrap; gap: 16px; margin-bottom: 8px; font-size: 13px; }
.delivery-desc { white-space: pre-wrap; line-height: 1.7; margin: 8px 0; }
.delivery-files { display: flex; flex-wrap: wrap; gap: 12px; align-items: center; margin-bottom: 12px; }
.file-link { display: inline-flex; align-items: center; gap: 4px; }
.reject-alert { margin: 8px 0; }
.panel-actions { margin-top: 16px; display: flex; gap: 12px; }
</style>
