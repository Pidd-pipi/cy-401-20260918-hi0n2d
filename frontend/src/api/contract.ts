import { getData, postData } from './request';
import type { Contract, ContractDelivery, SubmitDeliveryPayload } from '../types';

export const contractApi = {
  list: () => getData<Contract[]>('/contracts'),
  detail: (id: number) => getData<Contract>(`/contracts/${id}`),
  sign: (id: number) => postData<Contract>(`/contracts/${id}/sign`),
  complete: (id: number) => postData<Contract>(`/contracts/${id}/complete`),
  submitDelivery: (id: number, payload: SubmitDeliveryPayload) =>
    postData<ContractDelivery>(`/contracts/${id}/deliveries`, payload),
  rejectDelivery: (id: number, deliveryId: number, reason: string) =>
    postData<ContractDelivery>(`/contracts/${id}/deliveries/${deliveryId}/reject`, { reason }),
  acceptDelivery: (id: number, deliveryId: number) =>
    postData<ContractDelivery>(`/contracts/${id}/deliveries/${deliveryId}/accept`)
};
