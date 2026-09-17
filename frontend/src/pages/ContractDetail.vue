<template>
  <div class="page">
    <el-page-header content="合同详情" @back="$router.push('/dashboard')" />
    <el-card v-if="contract" class="detail-card">
      <div class="c-head">
        <h2>{{ contract.contractNo }}</h2>
        <StatusBadge :status="contract.status" kind="contract" />
      </div>
      <p class="muted">需求：{{ contract.requirement?.title }}</p>
      <p><b>{{ formatCurrency(contract.totalAmount) }}</b>
        <span class="muted"> · {{ contract.paymentType === 'installments' ? '分阶段付款' : '一次性付款' }}</span>
      </p>
      <div class="c-parties">
        <span>甲方：{{ contract.partyA?.name }}</span>
        <span>乙方：{{ contract.partyB?.name }}</span>
      </div>
      <div class="actions">
        <el-button v-if="contract.status === 'pending_signature'" type="primary" @click="sign">签署确认</el-button>
        <el-button v-if="contract.status === 'in_progress' && isPartyA && !hasActiveDelivery" type="success" @click="complete">确认完成</el-button>
      </div>
    </el-card>

    <el-card v-if="contract" class="stages-card">
      <template #header><b>阶段进度</b></template>
      <ProgressSteps :stages="contract.stages" />
    </el-card>

    <!-- 乙方：执行中可提交交付 -->
    <el-card v-if="contract && isPartyB && canSubmit" class="delivery-card">
      <template #header><b>提交交付</b></template>
      <el-form :model="form" label-width="90px">
        <el-form-item label="交付说明">
          <el-input
            v-model="form.description"
            type="textarea"
            :rows="4"
            maxlength="1000"
            show-word-limit
            placeholder="说明本次交付的内容、完成情况与验收要点（至少 5 字）"
          />
        </el-form-item>
        <el-form-item label="附件">
          <div class="attachments">
            <div v-for="(file, index) in form.attachments" :key="index" class="attachment-row">
              <el-input v-model="form.attachments[index]" placeholder="附件地址或文件名，如 /uploads/delivery.zip" />
              <el-button text type="danger" @click="removeAttachment(index)">移除</el-button>
            </div>
            <el-button size="small" @click="addAttachment">+ 添加附件</el-button>
          </div>
        </el-form-item>
      </el-form>
      <el-button type="primary" :loading="submitting" @click="submit">提交交付</el-button>
    </el-card>

    <!-- 最新交付 -->
    <el-card v-if="contract && contract.latestDelivery" class="delivery-card">
      <template #header>
        <div class="delivery-head">
          <b>最新交付</b>
          <StatusBadge :status="contract.latestDelivery.status" kind="delivery" />
        </div>
      </template>
      <p class="delivery-desc">{{ contract.latestDelivery.description }}</p>
      <div v-if="contract.latestDelivery.attachments.length" class="delivery-files">
        <div v-for="(file, index) in contract.latestDelivery.attachments" :key="index" class="file-item">
          <el-link :href="file" target="_blank" type="primary">📎 {{ file }}</el-link>
        </div>
      </div>
      <el-descriptions :column="1" border size="small" class="delivery-meta">
        <el-descriptions-item label="提交人">{{ contract.partyB?.name }}</el-descriptions-item>
        <el-descriptions-item label="提交时间">{{ formatDate(contract.latestDelivery.createdAt) }}</el-descriptions-item>
        <el-descriptions-item v-if="contract.latestDelivery.status === 'rejected'" label="驳回原因">
          <span class="reject-reason">{{ contract.latestDelivery.rejectReason }}</span>
        </el-descriptions-item>
        <el-descriptions-item v-if="contract.latestDelivery.reviewedAt" label="验收时间">
          {{ formatDate(contract.latestDelivery.reviewedAt) }}
        </el-descriptions-item>
      </el-descriptions>

      <!-- 甲方：待验收时只能驳回或接受一次 -->
      <div v-if="isPartyA && canReview" class="review-actions">
        <el-button type="danger" plain @click="openReject">驳回</el-button>
        <el-button type="success" @click="accept">接受交付</el-button>
      </div>
    </el-card>

    <el-dialog v-model="rejectVisible" title="驳回交付" width="480px">
      <el-form label-width="80px">
        <el-form-item label="驳回原因" required>
          <el-input
            v-model="rejectReason"
            type="textarea"
            :rows="3"
            placeholder="请填写驳回原因（必填），乙方修改后可重新提交"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="rejectVisible = false">取消</el-button>
        <el-button type="danger" :loading="submitting" @click="reject">确认驳回</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { useRoute } from 'vue-router';
import { ElMessage, ElMessageBox } from 'element-plus';
import type { Contract } from '../types';
import { useContractStore } from '../stores/contract';
import { useUserStore } from '../stores/user';
import { contractApi } from '../api/contract';
import StatusBadge from '../components/common/StatusBadge.vue';
import ProgressSteps from '../components/common/ProgressSteps.vue';
import { formatCurrency, formatDate } from '../utils/formatCurrency';

const route = useRoute();
const store = useContractStore();
const userStore = useUserStore();
const contract = ref<Contract | null>(null);

const submitting = ref(false);
const form = reactive<{ description: string; attachments: string[] }>({ description: '', attachments: [] });
const rejectVisible = ref(false);
const rejectReason = ref('');

const isPartyA = computed(() => contract.value?.partyAId === userStore.user?.id);
const isPartyB = computed(() => contract.value?.partyBId === userStore.user?.id);
const latest = computed(() => contract.value?.latestDelivery ?? null);
const hasActiveDelivery = computed(() => latest.value?.status === 'submitted');
const canSubmit = computed(
  () => contract.value?.status === 'in_progress' && latest.value?.status !== 'submitted'
);
const canReview = computed(
  () => contract.value?.status === 'pending_review' && latest.value?.status === 'submitted'
);

async function load() {
  const id = Number(route.params.id);
  contract.value = await store.fetchDetail(id);
}

async function sign() {
  if (!contract.value) return;
  await contractApi.sign(contract.value.id);
  ElMessage.success('签署成功');
  void load();
}

async function complete() {
  if (!contract.value) return;
  await contractApi.complete(contract.value.id);
  ElMessage.success('已确认完成');
  void load();
}

function addAttachment() {
  form.attachments.push('');
}

function removeAttachment(index: number) {
  form.attachments.splice(index, 1);
}

async function submit() {
  if (!contract.value) return;
  if (form.description.trim().length < 5) {
    ElMessage.warning('请填写至少 5 个字的交付说明');
    return;
  }
  submitting.value = true;
  try {
    await contractApi.submitDelivery(contract.value.id, {
      description: form.description.trim(),
      attachments: form.attachments.map((a) => a.trim()).filter(Boolean)
    });
    ElMessage.success('交付已提交，等待甲方验收');
    form.description = '';
    form.attachments = [];
    await load();
  } finally {
    submitting.value = false;
  }
}

function openReject() {
  rejectReason.value = '';
  rejectVisible.value = true;
}

async function reject() {
  if (!contract.value || !latest.value) return;
  if (rejectReason.value.trim().length < 2) {
    ElMessage.warning('驳回原因为必填项');
    return;
  }
  submitting.value = true;
  try {
    await contractApi.rejectDelivery(contract.value.id, latest.value.id, rejectReason.value.trim());
    ElMessage.success('已驳回，合同回到执行中');
    rejectVisible.value = false;
    await load();
  } finally {
    submitting.value = false;
  }
}

async function accept() {
  if (!contract.value || !latest.value) return;
  try {
    await ElMessageBox.confirm('接受后合同即完成，不可撤销。确认接受本次交付？', '确认验收', {
      type: 'warning',
      confirmButtonText: '接受交付',
      cancelButtonText: '取消'
    });
  } catch {
    return;
  }
  await contractApi.acceptDelivery(contract.value.id, latest.value.id);
  ElMessage.success('已接受交付，合同完成');
  void load();
}

onMounted(() => void load());
</script>

<style scoped>
.detail-card { margin-bottom: 16px; }
.c-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px; }
.c-parties { display: flex; gap: 24px; margin: 12px 0; }
.actions { margin-top: 16px; }
.stages-card { margin-bottom: 16px; }
.delivery-card { margin-bottom: 16px; }
.delivery-head { display: flex; justify-content: space-between; align-items: center; }
.delivery-desc { white-space: pre-wrap; line-height: 1.7; color: #303133; margin: 0 0 12px; }
.delivery-files { margin-bottom: 12px; }
.file-item { margin-bottom: 4px; }
.delivery-meta { margin-top: 4px; }
.reject-reason { color: var(--el-color-danger); }
.review-actions { margin-top: 16px; text-align: right; }
.attachment-row { display: flex; gap: 8px; align-items: center; margin-bottom: 8px; }
.attachments { width: 100%; }
</style>
