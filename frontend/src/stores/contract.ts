import { defineStore } from 'pinia';
import type { Contract } from '../types';
import { contractApi } from '../api/contract';

export const useContractStore = defineStore('contract', {
  state: () => ({
    contracts: [] as Contract[],
    current: null as Contract | null
  }),
  actions: {
    async fetchList() {
      this.contracts = await contractApi.list();
      return this.contracts;
    },
    async fetchDetail(id: number) {
      this.current = await contractApi.detail(id);
      return this.current;
    },
    async submitDelivery(id: number, payload: { description: string; attachments: string[] }) {
      await contractApi.submitDelivery(id, payload);
      return this.fetchDetail(id);
    },
    async rejectDelivery(id: number, deliveryId: number, reason: string) {
      await contractApi.rejectDelivery(id, deliveryId, reason);
      return this.fetchDetail(id);
    },
    async acceptDelivery(id: number, deliveryId: number) {
      await contractApi.acceptDelivery(id, deliveryId);
      return this.fetchDetail(id);
    }
  }
});
