import { getData, postData } from './request';
import type { Contract, ContractDelivery } from '../types';

export interface SubmitDeliveryPayload {
  description: string;
  attachments: string[];
}

export interface ReviewDeliveryPayload {
  action: 'reject' | 'accept';
  rejectReason?: string;
}

export const contractApi = {
  list: () => getData<Contract[]>('/contracts'),
  detail: (id: number) => getData<Contract>(`/contracts/${id}`),
  sign: (id: number) => postData<Contract>(`/contracts/${id}/sign`),
  submitDelivery: (id: number, payload: SubmitDeliveryPayload) =>
    postData<ContractDelivery>(`/contracts/${id}/deliveries`, payload),
  latestDelivery: (id: number) =>
    getData<ContractDelivery | null>(`/contracts/${id}/deliveries/latest`),
  reviewDelivery: (id: number, payload: ReviewDeliveryPayload) =>
    postData<ContractDelivery>(`/contracts/${id}/deliveries/review`, payload)
};
