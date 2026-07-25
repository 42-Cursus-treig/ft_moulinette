#include "ft_list.h"
#include <unistd.h>

void	ft_list_reverse(t_list **begin_list);

static void	put_str(char *s)
{
	int	i;

	i = 0;
	while (s[i])
		i++;
	write(1, s, i);
}

static void	print_list_str(t_list *lst)
{
	while (lst)
	{
		put_str((char *)lst->data);
		write(1, "\n", 1);
		lst = lst->next;
	}
}

int	main(int argc, char **argv)
{
	t_list	*begin;
	t_list	*elem;
	int		i;

	begin = NULL;
	i = argc - 1;
	while (i >= 1)
	{
		elem = ft_create_elem(argv[i]);
		elem->next = begin;
		begin = elem;
		i--;
	}
	ft_list_reverse(&begin);
	print_list_str(begin);
	return (0);
}
